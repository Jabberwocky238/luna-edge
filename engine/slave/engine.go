package slave

import (
	"context"
	"fmt"
	"io"
	"net"
	"net/http"
	"strings"
	"sync/atomic"
	"time"

	"github.com/jabberwocky238/luna-edge/dns"
	"github.com/jabberwocky238/luna-edge/engine"
	"github.com/jabberwocky238/luna-edge/ingress"
	"github.com/jabberwocky238/luna-edge/repository/metadata"
	"google.golang.org/grpc"
	"google.golang.org/grpc/credentials/insecure"
)

// Config 定义 slave 模式核心配置。
type Config struct {
	NodeID            string
	MasterAddress     string
	SubscribeSnapshot bool
	RetryMinBackoff   time.Duration
	RetryMaxBackoff   time.Duration
	DNSListenAddr     string
	DNSForwardEnabled bool
	DNSForwardServers []string
	DNSForwardTimeout time.Duration
	DNSGeoIPEnabled   bool
	DNSGeoIPMMDBPath  string
	DNSK8sEnabled     bool
	DNSK8sNamespace   string
	IngressHTTPAddr   string
	IngressTLSAddr    string
	IngressK8sEnabled bool
	IngressK8sNS      string
	IngressK8sClass   string
	IngressLRUSize    int
	HealthListenAddr  string
}

type Reader interface {
	GetCertificateBundle(ctx context.Context, hostname string, revision uint64) (*engine.CertificateBundle, error)
	GetDomainEntryByHostname(ctx context.Context, hostname string) (*metadata.DomainEntryProjection, error)
	GetDNSRecordsByHostname(ctx context.Context, hostname string) ([]metadata.DNSRecord, error)
	ListDNSRecords(ctx context.Context) ([]metadata.DNSRecord, error)
	GetSnapshotRecordID(ctx context.Context) (uint64, error)
}

type Writer interface {
	ApplySnapshot(ctx context.Context, snapshot *engine.Snapshot) error
}

type Store interface {
	Reader
	Writer
}

// Engine 是 slave 模式核心。
type Engine struct {
	Config     Config
	CacheRoot  string
	Cache      Reader
	Subscriber engine.Subscriber
	Applier    engine.SnapshotApplier
	ClientConn *grpc.ClientConn
	ready      atomic.Bool

	// 核心组件
	DNS     *dns.Engine
	Ingress *ingress.Engine

	// 健康检查服务器
	healthServer   *http.Server
	healthListener net.Listener
}

type CertificateSnapshotSyncer interface {
	SyncSnapshotCertificates(ctx context.Context, snapshot *engine.Snapshot) error
}

type CertificateRootProvider interface {
	CertificatesRoot() string
}

// New 创建 slave engine。
func New(cfg Config, cacheRoot string, cache Reader, applier engine.SnapshotApplier) (*Engine, error) {
	cacheRoot = strings.TrimSpace(cacheRoot)
	if cacheRoot == "" {
		return nil, fmt.Errorf("cache root is required")
	}
	if cfg.RetryMinBackoff <= 0 {
		cfg.RetryMinBackoff = time.Second
	}
	if cfg.RetryMaxBackoff <= 0 {
		cfg.RetryMaxBackoff = 30 * time.Second
	}
	conn, err := grpc.NewClient(cfg.MasterAddress, grpc.WithTransportCredentials(insecure.NewCredentials()))
	if err != nil {
		return nil, err
	}
	client := engine.NewGRPCClient(conn)
	subscriber := &streamSubscriber{
		Client:  client,
		Applier: applier,
	}
	if cfg.NodeID == "" {
		_ = conn.Close()
		return nil, fmt.Errorf("node id is required")
	}
	eng := &Engine{
		Config:     cfg,
		CacheRoot:  cacheRoot,
		Cache:      cache,
		Subscriber: subscriber,
		Applier:    applier,
		ClientConn: conn,
	}
	eng.ready.Store(false)
	subscriber.OnSnapshot = func(ctx context.Context, snapshot *engine.Snapshot) error {
		return eng.refreshRuntimeOnSnapshot(ctx, snapshot)
	}
	subscriber.OnConnect = func() { eng.ready.Store(true) }
	subscriber.OnDisconnect = func() { eng.ready.Store(false) }
	if store, ok := applier.(interface {
		SetCertificateBundleFetcher(CertificateBundleFetcher)
	}); ok {
		store.SetCertificateBundleFetcher(client)
	}
	if eng.Cache != nil && cfg.DNSListenAddr != "" {
		eng.DNS = dns.NewEngine(dns.EngineOptions{
			Forwarder: dns.ForwarderConfig{
				Enabled: cfg.DNSForwardEnabled,
				Servers: cfg.DNSForwardServers,
				Timeout: cfg.DNSForwardTimeout,
			},
			GeoIPEnabled:  cfg.DNSGeoIPEnabled,
			GeoIPMMDBPath: cfg.DNSGeoIPMMDBPath,
			K8sEnabled:    cfg.DNSK8sEnabled,
			K8sNamespace:  cfg.DNSK8sNamespace,
		})
		if err := eng.restoreDNSRuntime(context.Background()); err != nil {
			_ = conn.Close()
			return nil, err
		}
	}
	if cfg.IngressHTTPAddr != "" || cfg.IngressTLSAddr != "" {
		certRoot := certificatesRootFor(eng.CacheRoot)
		if provider, ok := applier.(CertificateRootProvider); ok {
			certRoot = provider.CertificatesRoot()
		}
		resolver, err := ingress.NewLunaTLSCertResolver(certRoot, cfg.IngressLRUSize)
		if err != nil {
			_ = conn.Close()
			return nil, err
		}
		ing, err := ingress.NewEngine(ingress.EngineOptions{
			HTTPListenAddr:  cfg.IngressHTTPAddr,
			TLSListenAddr:   cfg.IngressTLSAddr,
			K8sEnabled:      cfg.IngressK8sEnabled,
			K8sNamespace:    cfg.IngressK8sNS,
			K8sIngressClass: cfg.IngressK8sClass,
			LRUSize:         cfg.IngressLRUSize,
		}, resolver)
		if err != nil {
			_ = conn.Close()
			return nil, err
		}
		ing.InjectSlave(eng)
		eng.Ingress = ing
	}
	return eng, nil
}

func (e *Engine) ReadCache() ingress.RouteLookupReader {
	return e.Cache
}

// Subscribe 拉取 master 复制流。
func (e *Engine) Subscribe(ctx context.Context) error {
	if e.Subscriber == nil {
		return fmt.Errorf("subscriber is not configured")
	}
	return e.Subscriber.Subscribe(ctx, e.Config.NodeID)
}

// Start 启动复制订阅，并在失败时指数退避重试。
func (e *Engine) Start(ctx context.Context) error {
	if err := e.startHealthServer(); err != nil {
		return err
	}
	if e.DNS != nil {
		if err := e.DNS.Listen(e.Config.DNSListenAddr); err != nil {
			_ = e.stopHealthServer(context.Background())
			return err
		}
	}
	if e.Ingress != nil {
		if err := e.Ingress.Listen(); err != nil {
			if e.DNS != nil {
				_ = e.DNS.Stop()
			}
			_ = e.stopHealthServer(context.Background())
			return err
		}
	}
	backoff := e.Config.RetryMinBackoff
	for {
		err := e.Subscribe(ctx)
		if err == nil || ctx.Err() != nil {
			e.ready.Store(false)
			return err
		}
		e.ready.Store(false)
		timer := time.NewTimer(backoff)
		select {
		case <-ctx.Done():
			timer.Stop()
			return ctx.Err()
		case <-timer.C:
		}
		backoff *= 2
		if backoff > e.Config.RetryMaxBackoff {
			backoff = e.Config.RetryMaxBackoff
		}
	}
}

type streamSubscriber struct {
	Client       engine.Client
	Applier      engine.SnapshotApplier
	OnSnapshot   func(context.Context, *engine.Snapshot) error
	OnConnect    func()
	OnDisconnect func()
}

func (s *streamSubscriber) Subscribe(ctx context.Context, nodeID string) error {
	if s.Client == nil {
		return fmt.Errorf("replication client is not configured")
	}
	if s.Applier == nil {
		return fmt.Errorf("replication applier is not configured")
	}
	stream, err := s.Client.Subscribe(ctx, nodeID)
	if err != nil {
		return err
	}
	if s.OnConnect != nil {
		s.OnConnect()
	}
	defer func() {
		if s.OnDisconnect != nil {
			s.OnDisconnect()
		}
	}()
	var cursor uint64
	if provider, ok := s.Applier.(interface {
		GetSnapshotRecordID(context.Context) (uint64, error)
	}); ok {
		value, err := provider.GetSnapshotRecordID(ctx)
		if err != nil {
			return err
		}
		cursor = value
	}
	if err := s.catchUpSnapshots(ctx, nodeID, cursor); err != nil {
		return err
	}

	for {
		notice, recvErr := stream.Recv()
		if recvErr != nil {
			if recvErr == io.EOF {
				return nil
			}
			return recvErr
		}
		if notice == nil {
			continue
		}
		if err := s.catchUpSnapshots(ctx, nodeID, notice.SnapshotRecordID-1); err != nil {
			return err
		}
	}
}

func (s *streamSubscriber) catchUpSnapshots(ctx context.Context, nodeID string, cursor uint64) error {
	snapshotStream, err := s.Client.GetSnapshot(ctx, nodeID, cursor)
	if err != nil {
		return err
	}
	for {
		snapshot, recvErr := snapshotStream.Recv()
		if recvErr != nil {
			if recvErr == io.EOF {
				return nil
			}
			return recvErr
		}
		if snapshot == nil {
			continue
		}
		if err := s.Applier.ApplySnapshot(ctx, snapshot); err != nil {
			return err
		}
		if s.OnSnapshot != nil {
			if err := s.OnSnapshot(ctx, snapshot); err != nil {
				return err
			}
		}
		if snapshot.Last {
			return nil
		}
	}
}

// Stop 关闭 slave engine。
func (e *Engine) Stop(ctx context.Context) error {
	var firstErr error
	e.ready.Store(false)
	if err := e.stopHealthServer(ctx); err != nil && firstErr == nil {
		firstErr = err
	}
	if e.Ingress != nil {
		if err := e.Ingress.Stop(ctx); err != nil && firstErr == nil {
			firstErr = err
		}
	}
	if e.DNS != nil {
		if err := e.DNS.Stop(); err != nil && firstErr == nil {
			firstErr = err
		}
	}
	if e.ClientConn != nil {
		if err := e.ClientConn.Close(); err != nil && firstErr == nil {
			firstErr = err
		}
	}
	return firstErr
}

func (e *Engine) startHealthServer() error {
	if strings.TrimSpace(e.Config.HealthListenAddr) == "" {
		return nil
	}
	mux := http.NewServeMux()
	mux.HandleFunc("/healthz", e.handleHealthz)
	server := &http.Server{
		Addr:    e.Config.HealthListenAddr,
		Handler: mux,
	}
	lis, err := net.Listen("tcp", e.Config.HealthListenAddr)
	if err != nil {
		return err
	}
	e.healthServer = server
	e.healthListener = lis
	go func() {
		_ = server.Serve(lis)
	}()
	return nil
}

func (e *Engine) stopHealthServer(ctx context.Context) error {
	if e.healthServer == nil {
		return nil
	}
	err := e.healthServer.Shutdown(ctx)
	e.healthServer = nil
	e.healthListener = nil
	return err
}

func (e *Engine) handleHealthz(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodGet {
		http.Error(w, "method not allowed", http.StatusMethodNotAllowed)
		return
	}
	if !e.ready.Load() {
		http.Error(w, "master not connected", http.StatusServiceUnavailable)
		return
	}
	w.WriteHeader(http.StatusOK)
	_, _ = w.Write([]byte("ok\n"))
}

func (e *Engine) refreshRuntimeOnSnapshot(ctx context.Context, snapshot *engine.Snapshot) error {
	if syncer, ok := e.Applier.(CertificateSnapshotSyncer); ok {
		if err := syncer.SyncSnapshotCertificates(ctx, snapshot); err != nil {
			return err
		}
	}
	if e.DNS != nil {
		if err := e.restoreDNSRuntime(context.Background()); err != nil {
			return err
		}
	}
	if e.Ingress != nil {
		e.Ingress.RefreshAll()
	}
	return nil
}

func (e *Engine) restoreDNSRuntime(ctx context.Context) error {
	if e == nil || e.DNS == nil || e.Cache == nil {
		return nil
	}
	records, err := e.Cache.ListDNSRecords(ctx)
	if err != nil {
		return err
	}
	e.DNS.RestoreRecords(records)
	return nil
}
