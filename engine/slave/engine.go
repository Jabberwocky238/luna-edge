package slave

import (
	"context"
	"errors"
	"log"
	"net"
	"net/http"
	"strings"
	"sync/atomic"
	"time"

	"github.com/jabberwocky238/luna-edge/dns"
	"github.com/jabberwocky238/luna-edge/engine"
	"github.com/jabberwocky238/luna-edge/ingress"
	"github.com/jabberwocky238/luna-edge/replication"
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
	NODE_ID    string
	Config     *Config
	Cache      *LocalStore
	grpcClient *replication.GRPCClient
	ready      atomic.Bool

	// 核心组件
	DNS     *dns.Engine
	dnsChan chan []metadata.DNSRecord
	Ingress *ingress.Engine

	// 健康检查服务器
	healthServer   *http.Server
	healthListener net.Listener
}

// New 创建 slave engine。
func New(nodeID string, cfg *Config) (*Engine, error) {
	if cfg.RetryMinBackoff <= 0 {
		cfg.RetryMinBackoff = time.Second
	}
	if cfg.RetryMaxBackoff <= 0 {
		cfg.RetryMaxBackoff = 30 * time.Second
	}
	dnsChan := make(chan []metadata.DNSRecord, 100)
	eng := &Engine{NODE_ID: nodeID, Config: cfg, dnsChan: dnsChan}
	localStore, err := NewLocalStore(cfg.CacheRoot, eng, dnsChan)
	if err != nil {
		return nil, err
	}
	eng.Cache = localStore
	if cfg.DNSListenAddr != "" {
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
	}
	if cfg.IngressHTTPAddr != "" || cfg.IngressTLSAddr != "" {
		certRoot := eng.Cache.CertificatesRoot()
		resolver, err := ingress.NewLunaTLSCertResolver(certRoot, cfg.IngressLRUSize)
		if err != nil {
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
		ing.InjectSlave(eng)
		eng.Ingress = ing
	}
	return eng, nil
}

func (e *Engine) ReadCache() ingress.RouteLookupReader {
	return e.Cache
}

// Start 启动复制订阅，并在失败时指数退避重试。
func (e *Engine) Start(ctx context.Context) error {
	log.Printf("slave: start begin node_id=%s master=%s", e.NODE_ID, e.Config.MasterAddress)

	if e.DNS != nil {
		if err := e.DNS.BindContext(ctx); err != nil {
			return err
		}
		if err := e.DNS.Listen(e.Config.DNSListenAddr); err != nil {
			return err
		}
		defer e.DNS.Stop()
		go e.DNSRestoreLoop()
		defer close(e.dnsChan)
	}
	if e.Ingress != nil {
		if err := e.Ingress.BindContext(ctx); err != nil {
			return err
		}
		if err := e.Ingress.Listen(); err != nil {
			return err
		}
		defer e.Ingress.Stop()
	}
	if e.Cache != nil {
		if err := e.Cache.Start(); err != nil {
			return err
		}
		defer e.Cache.Close()
	}
	e.grpcClient = replication.NewGRPCClientEasy(e.Config.MasterAddress)
	if e.grpcClient == nil {
		return errors.New("failed to dial gRPC to master at " + e.Config.MasterAddress)
	}
	defer e.grpcClient.Close()
	if err := e.startHealthServer(); err != nil {
		return err
	}
	defer e.stopHealthServer(ctx)

	backoff := e.Config.RetryMinBackoff
	for {
		log.Printf("slave: subscribe attempt node_id=%s backoff=%s", e.NODE_ID, backoff)
		err := e.Subscribe(ctx, e.NODE_ID)
		if err == nil || ctx.Err() != nil {
			e.ready.Store(false)
			log.Printf("slave: subscribe loop finished node_id=%s err=%v ctx_err=%v", e.NODE_ID, err, ctx.Err())
			return err
		}
		e.ready.Store(false)
		log.Printf("slave: subscribe attempt failed node_id=%s err=%v next_backoff=%s", e.NODE_ID, err, backoff)
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

func (e *Engine) DNSRestoreLoop() {
	for items := range e.dnsChan {
		e.DNS.RestoreRecords(items)
	}
}
