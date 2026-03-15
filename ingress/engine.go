package ingress

import (
	"context"
	"fmt"
	"net/http"
	"net/http/httputil"
	"net/url"
	"strings"
	"sync"
	"time"

	enginepkg "github.com/jabberwocky238/luna-edge/engine"
	"github.com/jabberwocky238/luna-edge/repository/metadata"
)

const (
	defaultReadHeaderTimeout = 5 * time.Second
	defaultReadTimeout       = 15 * time.Second
	defaultWriteTimeout      = 30 * time.Second
	defaultIdleTimeout       = 60 * time.Second
)

// Engine 是基础 ingress 执行层。
//
// 它负责：
// - 从 k8s 内存资源和 slave readonly cache 读取域名到 service 的绑定关系
// - 将 HTTP 请求按 Host 转发到对应上游
// - 提供 Listen / Stop 生命周期控制
type Engine struct {
	opts        EngineOptions
	repo        ServiceBindingReader
	slave       ReplicaReader
	httpEngine  *HTTPEngine
	tlsEngine   *TLSEngine
	tlsResolver TLSCertResolver
	k8sBridge   *K8sBridge
	httpServer  *http.Server
	middlewares []Middleware
	memory      *memoryStore
	mu          sync.Mutex
}

// NewEngine 创建一个基础 ingress 引擎。
func NewEngine(opts EngineOptions, tlsResolver TLSCertResolver, middlewares ...Middleware) (*Engine, error) {
	if strings.TrimSpace(opts.HTTPListenAddr) == "" && strings.TrimSpace(opts.TLSListenAddr) == "" {
		return nil, fmt.Errorf("at least one ingress listen address is required")
	}
	if strings.TrimSpace(opts.TLSListenAddr) != "" && tlsResolver == nil {
		return nil, fmt.Errorf("tls certificate resolver is required when tls listen address is configured")
	}

	engine := &Engine{
		opts:        opts,
		tlsResolver: tlsResolver,
		middlewares: middlewares,
		memory:      newMemoryStore(),
	}

	engine.httpServer = &http.Server{
		Addr:              opts.HTTPListenAddr,
		Handler:           engine.NewHTTPHandler(),
		ReadHeaderTimeout: defaultReadHeaderTimeout,
		ReadTimeout:       defaultReadTimeout,
		WriteTimeout:      defaultWriteTimeout,
		IdleTimeout:       defaultIdleTimeout,
	}

	if strings.TrimSpace(opts.HTTPListenAddr) != "" {
		httpEngine, err := NewHTTPEngine(opts.HTTPListenAddr, engine.httpServer)
		if err != nil {
			return nil, err
		}
		engine.httpEngine = httpEngine
	}

	if strings.TrimSpace(opts.TLSListenAddr) != "" {
		tlsServer := &http.Server{
			Addr:              opts.TLSListenAddr,
			Handler:           engine.NewHTTPHandler(),
			ReadHeaderTimeout: defaultReadHeaderTimeout,
			ReadTimeout:       defaultReadTimeout,
			WriteTimeout:      defaultWriteTimeout,
			IdleTimeout:       defaultIdleTimeout,
		}
		tlsEngine, err := NewTLSEngine(engine.tlsResolver, tlsServer, engine.k8sBridge)
		if err != nil {
			return nil, err
		}
		engine.tlsEngine = tlsEngine
	}

	if opts.K8sEnabled {
		bridge, err := NewK8sBridge(opts.K8sNamespace, opts.K8sIngressClass)
		if err != nil {
			return nil, err
		}
		if err := bridge.LoadInitial(context.Background()); err != nil {
			return nil, err
		}
		engine.k8sBridge = bridge
		if engine.tlsEngine != nil {
			engine.tlsEngine.SetK8sBridge(bridge)
		}
	}

	return engine, nil
}

// Listen 启动已经挂接到 Engine 的 HTTP/TLS 子引擎。
func (e *Engine) Listen() error {
	e.mu.Lock()
	defer e.mu.Unlock()

	if e.httpEngine == nil && e.tlsEngine == nil {
		return fmt.Errorf("no ingress sub-engine configured")
	}
	if e.httpEngine != nil {
		if err := e.httpEngine.Listen(); err != nil {
			return err
		}
	}
	if e.tlsEngine != nil {
		if err := e.tlsEngine.Listen(); err != nil {
			return err
		}
	}
	if e.k8sBridge != nil {
		e.k8sBridge.Listen()
	}
	return nil
}

// Stop 停止 ingress 监听。
func (e *Engine) Stop(ctx context.Context) error {
	e.mu.Lock()
	defer e.mu.Unlock()

	var errs []string
	if e.httpEngine != nil {
		if err := e.httpEngine.Stop(ctx); err != nil {
			errs = append(errs, err.Error())
		}
	}
	if e.tlsEngine != nil {
		if err := e.tlsEngine.Stop(ctx); err != nil {
			errs = append(errs, err.Error())
		}
	}
	if e.k8sBridge != nil {
		if err := e.k8sBridge.Stop(); err != nil {
			errs = append(errs, err.Error())
		}
	}
	if len(errs) > 0 {
		return fmt.Errorf("ingress stop failed: %s", strings.Join(errs, "; "))
	}
	return nil
}

// Route 根据请求 Host 和 Path 查找上游。
func (e *Engine) Route(ctx context.Context, host, requestPath string) (*RouteResult, error) {
	return e.routeByKind(ctx, host, requestPath, metadata.ServiceBindingRouteKindHTTP)
}

func (e *Engine) RouteHTTPS(ctx context.Context, host, requestPath string) (*RouteResult, error) {
	return e.routeByKind(ctx, host, requestPath, metadata.ServiceBindingRouteKindHTTPS)
}

func (e *Engine) routeByKind(ctx context.Context, host, requestPath string, kind metadata.ServiceBindingRouteKind) (*RouteResult, error) {
	hostname := normalizeHost(host)
	if hostname == "" {
		return nil, fmt.Errorf("host is required")
	}

	if e.k8sBridge != nil {
		switch kind {
		case metadata.ServiceBindingRouteKindHTTPS:
			if resolved, ok := e.k8sBridge.ResolveHTTPS(hostname, requestPath); ok {
				return e.resultFromBinding(hostname, resolved.Binding)
			}
		default:
			if binding, _, ok := e.k8sBridge.ResolveRequest(hostname, requestPath); ok {
				return e.resultFromBinding(hostname, binding)
			}
		}
	}

	if binding, ok := e.memory.Get(hostname); ok {
		return e.resultFromBinding(hostname, binding)
	}

	if e.slave != nil {
		binding, err := lookupBindingFromReadOnlyCache(ctx, e.slave, hostname)
		if err == nil && binding != nil {
			return e.resultFromReplicaBinding(hostname, binding)
		}
		return nil, err
	}

	if e.repo != nil {
		if binding, err := e.repo.GetServiceBindingByHostname(ctx, hostname); err == nil && binding != nil {
			return e.resultFromBinding(hostname, binding)
		}
	}
	return &RouteResult{Found: false}, nil
}

func (e *Engine) serveHTTP(w http.ResponseWriter, r *http.Request) {
	var (
		result *RouteResult
		err    error
	)
	if strings.EqualFold(r.Header.Get("X-Forwarded-Proto"), "https") {
		result, err = e.RouteHTTPS(r.Context(), r.Host, r.URL.Path)
	} else {
		result, err = e.Route(r.Context(), r.Host, r.URL.Path)
	}
	if err != nil {
		http.Error(w, err.Error(), http.StatusInternalServerError)
		return
	}
	if result == nil || !result.Found {
		http.NotFound(w, r)
		return
	}

	targetURL, err := url.Parse(result.Target.UpstreamURL)
	if err != nil {
		http.Error(w, "invalid upstream url", http.StatusInternalServerError)
		return
	}

	proxy := httputil.NewSingleHostReverseProxy(targetURL)
	originalDirector := proxy.Director
	proxy.Director = func(req *http.Request) {
		originalDirector(req)
		req.Host = targetURL.Host
	}
	proxy.ErrorHandler = func(rw http.ResponseWriter, req *http.Request, proxyErr error) {
		http.Error(rw, proxyErr.Error(), http.StatusBadGateway)
	}
	proxy.ServeHTTP(w, r)
}

func (e *Engine) resultFromBinding(hostname string, binding *metadata.ServiceBinding) (*RouteResult, error) {
	upstreamURL := buildUpstreamURL(string(binding.Protocol), binding.Address, binding.Port)
	if upstreamURL == "" {
		return nil, fmt.Errorf("service binding %q has no valid upstream", binding.ID)
	}
	result := &RouteResult{
		Found: true,
		Target: ProxyTarget{
			Hostname:    hostname,
			UpstreamURL: upstreamURL,
			Protocol:    string(binding.Protocol),
		},
	}
	e.memory.Put(binding)
	return result, nil
}

func (e *Engine) resultFromReplicaBinding(hostname string, binding *enginepkg.BindingRecord) (*RouteResult, error) {
	upstreamURL := buildUpstreamURL(binding.Protocol, binding.Address, binding.Port)
	if upstreamURL == "" {
		return nil, fmt.Errorf("replica binding %q has no valid upstream", binding.ID)
	}
	result := &RouteResult{
		Found: true,
		Target: ProxyTarget{
			Hostname:    hostname,
			UpstreamURL: upstreamURL,
			Protocol:    binding.Protocol,
		},
	}
	e.memory.Put(&metadata.ServiceBinding{
		ID:           binding.ID,
		DomainID:     binding.DomainID,
		Hostname:     binding.Hostname,
		ServiceID:    binding.ServiceID,
		Namespace:    binding.Namespace,
		Name:         binding.Name,
		Address:      binding.Address,
		Port:         binding.Port,
		Protocol:     metadata.ServiceBindingRouteKind(binding.Protocol),
		RouteVersion: binding.RouteVersion,
		BackendJSON:  binding.BackendJSON,
	})
	return result, nil
}

// InjectRepository 在初始化后注入只读 service binding 仓储。
func (e *Engine) InjectRepository(repo ServiceBindingReader) {
	e.mu.Lock()
	defer e.mu.Unlock()
	e.repo = repo
}

// InjectSlave 在初始化后注入 slave，用于读取本地只读缓存。
func (e *Engine) InjectSlave(slave ReplicaReader) {
	e.mu.Lock()
	defer e.mu.Unlock()
	e.slave = slave
}

func (e *Engine) RefreshAll() {
	e.mu.Lock()
	defer e.mu.Unlock()
	if e.memory != nil {
		e.memory.Clear()
	}
	if resolver, ok := e.tlsResolver.(*LunaTLSCertResolver); ok {
		resolver.Clear()
	}
}

func (e *Engine) RefreshHost(hostname string) {
	e.mu.Lock()
	defer e.mu.Unlock()
	if e.memory != nil {
		e.memory.Delete(hostname)
	}
}

func (e *Engine) RefreshCertificateHost(hostname string) {
	e.mu.Lock()
	defer e.mu.Unlock()
	if resolver, ok := e.tlsResolver.(*LunaTLSCertResolver); ok {
		resolver.DeleteCandidates(hostname)
	}
}
