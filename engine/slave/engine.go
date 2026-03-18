package slave

import (
	"context"
	"fmt"
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
)

// Config 定义 slave 模式核心配置。
type Config struct {
	CacheRoot     string
	MasterAddress string

	RetryMinBackoff time.Duration
	RetryMaxBackoff time.Duration

	DNSListenAddr     string
	DNSForwardEnabled bool
	DNSForwardServers []string
	DNSForwardTimeout time.Duration
	DNSGeoIPEnabled   bool
	DNSGeoIPMMDBPath  string
	DNSK8sEnabled     bool

	IngressHTTPAddr   string
	IngressTLSAddr    string
	IngressK8sEnabled bool
	IngressK8sNS      string
	IngressK8sClass   string
	IngressLRUSize    int

	MasterManageURL  string
	HealthListenAddr string
}

// Engine 是 slave 模式核心。
type Engine struct {
	Config *Config
	Cache  *LocalStore
	Client *engine.Client
	ready  atomic.Bool

	// 核心组件
	DNS     *dns.Engine
	Ingress *ingress.Engine

	// 健康检查服务器
	healthServer   *http.Server
	healthListener net.Listener
}

type IngressRouteLookupReader interface {
	GetDomainEntryByHostname(ctx context.Context, hostname string) (*metadata.DomainEntryProjection, error)
}
type SlaveStore interface {
	ReadCache() IngressRouteLookupReader
}

// New 创建 slave engine。
func New(cfg *Config) (*Engine, error) {
	localStore, err := NewLocalStore(cfg.CacheRoot)
	if err != nil {
		return nil, err
	}
	if cfg.RetryMinBackoff <= 0 {
		cfg.RetryMinBackoff = time.Second
	}
	if cfg.RetryMaxBackoff <= 0 {
		cfg.RetryMaxBackoff = 30 * time.Second
	}
	eng := &Engine{
		Config:     cfg,
		Cache:      localStore,
		Subscriber: subscriber,
	}
	eng.ready.Store(false)
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
			K8sNamespace:  engine.POD_NAMESPACE,
		})
		if err := eng.restoreDNSRuntime(context.Background()); err != nil {
			_ = conn.Close()
			return nil, err
		}
	}
	if cfg.IngressHTTPAddr != "" || cfg.IngressTLSAddr != "" {
		certRoot := eng.Cache.CertificatesRoot()
		resolver, err := ingress.NewLunaTLSCertResolver(certRoot, cfg.IngressLRUSize)
		if err != nil {
			_ = conn.Close()
			return nil, err
		}
		ing, err := ingress.NewEngine(ingress.EngineOptions{
			HTTPListenAddr:       cfg.IngressHTTPAddr,
			TLSListenAddr:        cfg.IngressTLSAddr,
			K8sEnabled:           cfg.IngressK8sEnabled,
			K8sNamespace:         engine.POD_NAMESPACE,
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
	if e.Client != nil {
		if err := e.Client.Close(); err != nil && firstErr == nil {
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
	if syncer, ok := e.Applier.(CertificateSyncer); ok {
		for _, changelog := range changelogsFromSnapshot(snapshot) {
			if err := syncer.SyncChangelogCertificates(ctx, changelog); err != nil {
				log.Printf("slave: sync changelog certificates failed snapshot_record_id=%d err=%v", snapshot.SnapshotRecordID, err)
				return err
			}
		}
		log.Printf("slave: sync changelog certificates done snapshot_record_id=%d", snapshot.SnapshotRecordID)
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
