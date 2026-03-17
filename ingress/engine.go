package ingress

import (
	"context"
	"fmt"
	"net/http"
	"strings"
	"sync"
	"time"

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
	repo        ProjectionReader
	slave       ReplicaReader
	httpEngine  *HTTPEngine
	tlsEngine   *TLSEngine
	tlsResolver TLSCertResolver
	k8sBridge   *K8sBridge
	httpServer  *http.Server
	middlewares []Middleware
	memory      *memoryStore
	mu          sync.Mutex
	ctx         context.Context
	k8sLoaded   bool
}

const acmeHTTP01Prefix = "/.well-known/acme-challenge/"

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
		tlsEngine.SetMemoryStore(engine.memory)
		engine.tlsEngine = tlsEngine
	}

	if opts.K8sEnabled {
		bridge, err := NewK8sBridge(opts.K8sNamespace, opts.K8sIngressClass)
		if err != nil {
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
func (e *Engine) BindContext(ctx context.Context) error {
	if e == nil {
		return nil
	}
	if ctx == nil {
		ctx = context.Background()
	}
	e.ctx = ctx
	if e.k8sBridge != nil && !e.k8sLoaded {
		if err := e.k8sBridge.LoadInitial(ctx); err != nil {
			return err
		}
		e.k8sLoaded = true
	}
	return nil
}

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
		e.k8sBridge.Listen(e.runtimeContext())
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

func (e *Engine) runtimeContext() context.Context {
	if e != nil && e.ctx != nil {
		return e.ctx
	}
	return context.Background()
}

// Route 根据请求 Host 和 Path 查找上游。
func (e *Engine) Route(ctx context.Context, host, requestPath string) (*RouteResult, error) {
	return e.routeByKind(ctx, host, requestPath, RouteKindHTTP)
}

func (e *Engine) RouteHTTPS(ctx context.Context, host, requestPath string) (*RouteResult, error) {
	return e.routeByKind(ctx, host, requestPath, RouteKindHTTPS)
}

func (e *Engine) routeByKind(ctx context.Context, host, requestPath string, kind RouteKind) (*RouteResult, error) {
	hostname := normalizeHost(host)
	if hostname == "" {
		return nil, fmt.Errorf("host is required")
	}

	if e.k8sBridge != nil {
		switch kind {
		case RouteKindHTTPS:
			if resolved, ok := e.k8sBridge.ResolveHTTPS(hostname, requestPath); ok {
				return e.resultFromBinding(hostname, resolved.Binding)
			}
		default:
			if binding, _, ok := e.k8sBridge.ResolveRequest(hostname, requestPath); ok {
				return e.resultFromBinding(hostname, binding)
			}
		}
	}

	if binding, ok := e.memory.Get(hostname, requestPath); ok {
		return e.resultFromBinding(hostname, binding)
	}

	if e.slave != nil {
		entry, err := lookupRouteFromReadOnlyCache(ctx, e.slave, hostname)
		if err == nil && entry != nil {
			return e.resultFromProjection(hostname, requestPath, kind, entry)
		}
		return nil, err
	}

	if e.repo != nil {
		entry, err := e.repo.GetDomainEntryProjectionByDomain(ctx, hostname)
		if err == nil && entry != nil {
			return e.resultFromProjection(hostname, requestPath, kind, entry)
		}
	}
	return &RouteResult{Found: false}, nil
}

func (e *Engine) resultFromBinding(hostname string, binding *BackendBinding) (*RouteResult, error) {
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

func (e *Engine) resultFromProjection(hostname, requestPath string, kind RouteKind, entry *metadata.DomainEntryProjection) (*RouteResult, error) {
	binding := bindingFromProjection(entry, requestPath, kind)
	if binding == nil {
		return &RouteResult{Found: false}, nil
	}
	return e.resultFromBinding(hostname, binding)
}

// InjectRepository 在初始化后注入只读 projection 仓储。
func (e *Engine) InjectRepository(repo ProjectionReader) {
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

func bindingFromProjection(entry *metadata.DomainEntryProjection, requestPath string, kind RouteKind) *BackendBinding {
	if entry == nil {
		return nil
	}
	switch kind {
	case RouteKindHTTP, RouteKindHTTPS, RouteKindGRPC:
		route := selectHTTPRoute(entry.HTTPRoutes, requestPath)
		if route == nil || route.BackendRef == nil {
			return nil
		}
		upstreamProtocol := RouteKindHTTP
		if kind == RouteKindGRPC {
			upstreamProtocol = RouteKindGRPC
		}
		return &BackendBinding{
			ID:            route.ID,
			DomainID:      entry.ID,
			Hostname:      entry.Hostname,
			Namespace:     route.BackendRef.ServiceNamespace,
			Name:          route.BackendRef.ServiceName,
			Address:       buildServiceAddress(route.BackendRef.ServiceName, route.BackendRef.ServiceNamespace),
			Port:          route.BackendRef.ServicePort,
			Protocol:      upstreamProtocol,
			RouteVersion:  1,
			Path:          route.Path,
			Priority:      route.Priority,
			BackendRef:    route.BackendRef,
			DomainEntryID: entry.ID,
		}
	case RouteKindTLSTerminate, RouteKindTLSPassthrough, RouteKindTCP, RouteKindUDP:
		if entry.BindedBackendRef == nil {
			return nil
		}
		return &BackendBinding{
			ID:            entry.BindedBackendRef.ID,
			DomainID:      entry.ID,
			Hostname:      entry.Hostname,
			Namespace:     entry.BindedBackendRef.ServiceNamespace,
			Name:          entry.BindedBackendRef.ServiceName,
			Address:       buildServiceAddress(entry.BindedBackendRef.ServiceName, entry.BindedBackendRef.ServiceNamespace),
			Port:          entry.BindedBackendRef.ServicePort,
			Protocol:      kind,
			BackendRef:    entry.BindedBackendRef,
			DomainEntryID: entry.ID,
		}
	default:
		return nil
	}
}

func selectHTTPRoute(routes []metadata.HTTPRouteProjection, requestPath string) *metadata.HTTPRouteProjection {
	var selected *metadata.HTTPRouteProjection
	for i := range routes {
		route := &routes[i]
		if !httpRouteMatches(route.Path, requestPath) {
			continue
		}
		if selected == nil || route.Priority > selected.Priority || (route.Priority == selected.Priority && len(route.Path) > len(selected.Path)) {
			selected = route
		}
	}
	return selected
}

func httpRouteMatches(path, requestPath string) bool {
	if path == "" || path == "/" {
		return true
	}
	if requestPath == "" {
		requestPath = "/"
	}
	return strings.HasPrefix(requestPath, path)
}
