package slave

import (
	"context"
	"fmt"
	"io"
	"log"
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
	MasterManageURL   string
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
			HTTPListenAddr:       cfg.IngressHTTPAddr,
			TLSListenAddr:        cfg.IngressTLSAddr,
			K8sEnabled:           cfg.IngressK8sEnabled,
			K8sNamespace:         cfg.IngressK8sNS,
			K8sIngressClass:      cfg.IngressK8sClass,
			LRUSize:              cfg.IngressLRUSize,
			MasterHTTP01ProxyURL: cfg.MasterManageURL,
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
	log.Printf("slave: subscribe begin node_id=%s master=%s", engine.POD_NAME, e.Config.MasterAddress)
	return e.Subscriber.Subscribe(ctx, engine.POD_NAME)
}

// Start 启动复制订阅，并在失败时指数退避重试。
func (e *Engine) Start(ctx context.Context) error {
	log.Printf("slave: start begin node_id=%s master=%s", engine.POD_NAME, e.Config.MasterAddress)
	if e.DNS != nil {
		if err := e.DNS.BindContext(ctx); err != nil {
			return err
		}
	}
	if e.Ingress != nil {
		if err := e.Ingress.BindContext(ctx); err != nil {
			return err
		}
	}
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
		log.Printf("slave: subscribe attempt node_id=%s backoff=%s", engine.POD_NAME, backoff)
		err := e.Subscribe(ctx)
		if err == nil || ctx.Err() != nil {
			e.ready.Store(false)
			log.Printf("slave: subscribe loop finished node_id=%s err=%v ctx_err=%v", engine.POD_NAME, err, ctx.Err())
			return err
		}
		e.ready.Store(false)
		log.Printf("slave: subscribe attempt failed node_id=%s err=%v next_backoff=%s", engine.POD_NAME, err, backoff)
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
		log.Printf("slave: open notice stream failed node_id=%s err=%v", nodeID, err)
		return err
	}
	log.Printf("slave: notice stream opened node_id=%s", nodeID)
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
	log.Printf("slave: local snapshot cursor node_id=%s cursor=%d", nodeID, cursor)
	if err := s.catchUpSnapshots(ctx, nodeID, cursor); err != nil {
		log.Printf("slave: initial catch-up failed node_id=%s cursor=%d err=%v", nodeID, cursor, err)
		return err
	}
	log.Printf("slave: initial catch-up done node_id=%s cursor=%d", nodeID, cursor)

	for {
		notice, recvErr := stream.Recv()
		if recvErr != nil {
			if recvErr == io.EOF {
				log.Printf("slave: notice stream closed by server node_id=%s", nodeID)
				return nil
			}
			log.Printf("slave: notice recv failed node_id=%s err=%v", nodeID, recvErr)
			return recvErr
		}
		if notice == nil {
			continue
		}
		log.Printf("slave: notice received node_id=%s snapshot_record_id=%d dns=%v domain=%v", nodeID, notice.SnapshotRecordID, notice.DNSRecord != nil, notice.DomainEntry != nil)
		if err := s.catchUpSnapshots(ctx, nodeID, notice.SnapshotRecordID-1); err != nil {
			log.Printf("slave: catch-up after notice failed node_id=%s snapshot_record_id=%d err=%v", nodeID, notice.SnapshotRecordID, err)
			return err
		}
	}
}

func (s *streamSubscriber) catchUpSnapshots(ctx context.Context, nodeID string, cursor uint64) error {
	log.Printf("slave: catch-up begin node_id=%s cursor=%d", nodeID, cursor)
	snapshotStream, err := s.Client.GetSnapshot(ctx, nodeID, cursor)
	if err != nil {
		log.Printf("slave: catch-up open snapshot stream failed node_id=%s cursor=%d err=%v", nodeID, cursor, err)
		return err
	}
	for {
		snapshot, recvErr := snapshotStream.Recv()
		if recvErr != nil {
			if recvErr == io.EOF {
				log.Printf("slave: catch-up stream eof node_id=%s cursor=%d", nodeID, cursor)
				return nil
			}
			log.Printf("slave: catch-up recv failed node_id=%s cursor=%d err=%v", nodeID, cursor, recvErr)
			return recvErr
		}
		if snapshot == nil {
			continue
		}
		log.Printf("slave: apply snapshot begin node_id=%s snapshot_record_id=%d last=%v dns=%d domains=%d", nodeID, snapshot.SnapshotRecordID, snapshot.Last, len(snapshot.DNSRecords), len(snapshot.DomainEntries))
		if err := s.Applier.ApplySnapshot(ctx, snapshot); err != nil {
			log.Printf("slave: apply snapshot failed node_id=%s snapshot_record_id=%d err=%v", nodeID, snapshot.SnapshotRecordID, err)
			return err
		}
		log.Printf("slave: apply snapshot done node_id=%s snapshot_record_id=%d", nodeID, snapshot.SnapshotRecordID)
		if s.OnSnapshot != nil {
			if err := s.OnSnapshot(ctx, snapshot); err != nil {
				log.Printf("slave: on snapshot hook failed node_id=%s snapshot_record_id=%d err=%v", nodeID, snapshot.SnapshotRecordID, err)
				return err
			}
			log.Printf("slave: on snapshot hook done node_id=%s snapshot_record_id=%d", nodeID, snapshot.SnapshotRecordID)
		}
		if snapshot.Last {
			log.Printf("slave: catch-up done node_id=%s snapshot_record_id=%d", nodeID, snapshot.SnapshotRecordID)
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
	log.Printf("slave: refresh runtime begin snapshot_record_id=%d dns=%d domains=%d", snapshot.SnapshotRecordID, len(snapshot.DNSRecords), len(snapshot.DomainEntries))
	if syncer, ok := e.Applier.(CertificateSnapshotSyncer); ok {
		if err := syncer.SyncSnapshotCertificates(ctx, snapshot); err != nil {
			log.Printf("slave: sync snapshot certificates failed snapshot_record_id=%d err=%v", snapshot.SnapshotRecordID, err)
			return err
		}
		log.Printf("slave: sync snapshot certificates done snapshot_record_id=%d", snapshot.SnapshotRecordID)
	}
	if e.DNS != nil {
		if err := e.restoreDNSRuntime(context.Background()); err != nil {
			log.Printf("slave: restore dns runtime failed snapshot_record_id=%d err=%v", snapshot.SnapshotRecordID, err)
			return err
		}
		log.Printf("slave: restore dns runtime done snapshot_record_id=%d", snapshot.SnapshotRecordID)
	}
	if e.Ingress != nil {
		e.Ingress.RefreshAll()
		log.Printf("slave: ingress runtime refreshed snapshot_record_id=%d", snapshot.SnapshotRecordID)
	}
	log.Printf("slave: refresh runtime done snapshot_record_id=%d", snapshot.SnapshotRecordID)
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
