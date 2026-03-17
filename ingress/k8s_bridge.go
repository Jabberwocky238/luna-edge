package ingress

import (
	"context"
	"fmt"
	"strings"
	"sync"

	enginepkg "github.com/jabberwocky238/luna-edge/engine"
	"k8s.io/client-go/dynamic"
	dynamicinformer "k8s.io/client-go/dynamic/dynamicinformer"
	"k8s.io/client-go/informers"
	"k8s.io/client-go/kubernetes"
	"k8s.io/client-go/rest"
)

// K8sBridge 统一监听标准 Ingress 与 Gateway API，并按协议种类物化后端。
type K8sBridge struct {
	namespace    string
	ingressClass string
	notifier     CertificateIntentNotifier

	client         kubernetes.Interface
	dynamicClient  dynamic.Interface
	ingressFactory informers.SharedInformerFactory
	gatewayFactory dynamicinformer.DynamicSharedInformerFactory
	stopCh         chan struct{}

	mu sync.RWMutex

	ingresses map[string]*k8sIngressState
	gateways  map[string]*k8sGatewayState

	httpRoutes map[string]*k8sHTTPRouteState
	grpcRoutes map[string]*k8sGRPCRouteState
	tlsRoutes  map[string]*k8sTLSRouteState
	tcpRoutes  map[string]*k8sTCPRouteState
	udpRoutes  map[string]*k8sUDPRouteState

	httpResolved  map[string][]k8sMaterializedRoute
	httpsResolved map[string][]k8sMaterializedRoute
	grpcResolved  map[string][]k8sMaterializedRoute
	tlsResolved   map[string][]k8sMaterializedRoute
	tcpResolved   map[uint32][]k8sMaterializedRoute
	udpResolved   map[uint32][]k8sMaterializedRoute
}

type k8sMaterializedRoute struct {
	kind       RouteKind
	binding    *BackendBinding
	route      *ResolvedRoute
	hostname   string
	port       uint32
	path       string
	pathKind   k8sRoutePathKind
	listener   string
	routeOrder int
}

type k8sRoutePathKind int

const (
	k8sRoutePathPrefix k8sRoutePathKind = iota
	k8sRoutePathExact
)

// NewK8sBridge 创建监听当前命名空间 Ingress 和 Gateway API 的 bridge。
func NewK8sBridge(namespace, ingressClass string) (*K8sBridge, error) {
	if namespace == "" {
		namespace = enginepkg.POD_NAMESPACE
	}
	if namespace == "" {
		namespace = "default"
	}

	cfg, err := rest.InClusterConfig()
	if err != nil {
		return nil, fmt.Errorf("create in-cluster k8s config: %w", err)
	}

	client, err := kubernetes.NewForConfig(cfg)
	if err != nil {
		return nil, fmt.Errorf("create k8s client: %w", err)
	}
	dynClient, err := dynamic.NewForConfig(cfg)
	if err != nil {
		return nil, fmt.Errorf("create dynamic k8s client: %w", err)
	}

	return NewK8sBridgeWithClients(namespace, ingressClass, client, dynClient), nil
}

// NewK8sBridgeWithClient 创建使用显式 typed client 的 bridge。
func NewK8sBridgeWithClient(namespace, ingressClass string, client kubernetes.Interface) *K8sBridge {
	return NewK8sBridgeWithClients(namespace, ingressClass, client, nil)
}

// NewK8sBridgeWithClients 创建使用显式 typed/dynamic client 的 bridge。
func NewK8sBridgeWithClients(namespace, ingressClass string, client kubernetes.Interface, dynamicClient dynamic.Interface) *K8sBridge {
	if namespace == "" {
		namespace = enginepkg.POD_NAMESPACE
	}
	if namespace == "" {
		namespace = "default"
	}

	bridge := &K8sBridge{
		namespace:     namespace,
		ingressClass:  strings.TrimSpace(ingressClass),
		client:        client,
		dynamicClient: dynamicClient,
		stopCh:        make(chan struct{}),
		ingresses:     make(map[string]*k8sIngressState),
		gateways:      make(map[string]*k8sGatewayState),
		httpRoutes:    make(map[string]*k8sHTTPRouteState),
		grpcRoutes:    make(map[string]*k8sGRPCRouteState),
		tlsRoutes:     make(map[string]*k8sTLSRouteState),
		tcpRoutes:     make(map[string]*k8sTCPRouteState),
		udpRoutes:     make(map[string]*k8sUDPRouteState),
		httpResolved:  make(map[string][]k8sMaterializedRoute),
		httpsResolved: make(map[string][]k8sMaterializedRoute),
		grpcResolved:  make(map[string][]k8sMaterializedRoute),
		tlsResolved:   make(map[string][]k8sMaterializedRoute),
		tcpResolved:   make(map[uint32][]k8sMaterializedRoute),
		udpResolved:   make(map[uint32][]k8sMaterializedRoute),
	}
	initBridgeHandlers(bridge)
	return bridge
}

// LoadInitial 全量加载当前命名空间已有的 Ingress 与 Gateway API 资源。
func (b *K8sBridge) LoadInitial(ctx context.Context) error {
	if err := b.loadInitialIngresses(ctx); err != nil {
		return err
	}
	if err := b.loadInitialGateways(ctx); err != nil {
		return err
	}

	b.mu.Lock()
	b.rebuildRoutesLocked()
	b.mu.Unlock()
	return nil
}

// Listen 启动 informer 监听当前命名空间资源变化。
func (b *K8sBridge) Listen() {
	if b.ingressFactory != nil {
		b.ingressFactory.Start(b.stopCh)
	}
	if b.gatewayFactory != nil {
		b.gatewayFactory.Start(b.stopCh)
	}
}

// Stop 停止 informer。
func (b *K8sBridge) Stop() error {
	select {
	case <-b.stopCh:
		return nil
	default:
		close(b.stopCh)
		return nil
	}
}

func (b *K8sBridge) SetCertificateIntentNotifier(notifier CertificateIntentNotifier) {
	b.mu.Lock()
	b.notifier = notifier
	hosts := b.collectCertificateIntentsLocked()
	b.mu.Unlock()
	b.notifyCertificateHosts(context.Background(), hosts)
}

// Namespace 返回 bridge 当前监听的命名空间。
func (b *K8sBridge) Namespace() string {
	return b.namespace
}

func (b *K8sBridge) collectCertificateIntentsLocked() []string {
	seen := map[string]struct{}{}
	var hosts []string
	for _, ing := range b.ingresses {
		for _, host := range ingressCertificateHosts(ing.resource) {
			if _, ok := seen[host]; ok {
				continue
			}
			seen[host] = struct{}{}
			hosts = append(hosts, host)
		}
	}
	for _, gateway := range b.gateways {
		for _, host := range gatewayCertificateHosts(gateway) {
			if _, ok := seen[host]; ok {
				continue
			}
			seen[host] = struct{}{}
			hosts = append(hosts, host)
		}
	}
	return hosts
}

func (b *K8sBridge) notifyCertificateHosts(ctx context.Context, hosts []string) {
	if b == nil || len(hosts) == 0 {
		return
	}
	b.mu.RLock()
	notifier := b.notifier
	b.mu.RUnlock()
	if notifier == nil {
		return
	}
	for _, host := range hosts {
		if host == "" {
			continue
		}
		_ = notifier.NotifyCertificateDesired(ctx, host)
	}
}

// ResolveHost 兼容旧接口，等价于 HTTP `/` 命中。
func (b *K8sBridge) ResolveHost(host string) (*BackendBinding, *ResolvedRoute, bool) {
	return b.ResolveRequest(host, "/")
}

// ResolveRequest 兼容旧接口，等价于 HTTPRoute / Ingress 解析。
func (b *K8sBridge) ResolveRequest(host, requestPath string) (*BackendBinding, *ResolvedRoute, bool) {
	resolved, ok := b.ResolveHTTP(host, requestPath)
	if !ok {
		return nil, nil, false
	}
	return cloneBinding(resolved.Binding), cloneRoute(resolved.Route), true
}

func (b *K8sBridge) ResolveHTTP(host, requestPath string) (*K8sResolvedBackend, bool) {
	return b.resolveHostPath(RouteKindHTTP, host, requestPath)
}

func (b *K8sBridge) ResolveGRPC(host, requestPath string) (*K8sResolvedBackend, bool) {
	return b.resolveHostPath(RouteKindGRPC, host, requestPath)
}

func (b *K8sBridge) ResolveHTTPS(host, requestPath string) (*K8sResolvedBackend, bool) {
	return b.resolveHostPath(RouteKindHTTPS, host, requestPath)
}

func (b *K8sBridge) ResolveTLS(serverName string) (*K8sResolvedBackend, bool) {
	return b.resolveHostPath(RouteKindTLSTerminate, serverName, "/")
}

func (b *K8sBridge) ResolveTLSPassthrough(serverName string) (*K8sResolvedBackend, bool) {
	return b.resolveHostPath(RouteKindTLSPassthrough, serverName, "/")
}

func (b *K8sBridge) ResolveTCP(port uint32) (*K8sResolvedBackend, bool) {
	return b.resolvePort(RouteKindTCP, port)
}

func (b *K8sBridge) ResolveUDP(port uint32) (*K8sResolvedBackend, bool) {
	return b.resolvePort(RouteKindUDP, port)
}

func (b *K8sBridge) resolveHostPath(kind RouteKind, host, requestPath string) (*K8sResolvedBackend, bool) {
	host = normalizeHost(host)
	if host == "" {
		return nil, false
	}
	requestPath = normalizeIngressPath(requestPath)

	b.mu.RLock()
	defer b.mu.RUnlock()

	var routes []k8sMaterializedRoute
	switch kind {
	case RouteKindHTTP:
		routes = b.httpResolved[host]
	case RouteKindHTTPS:
		routes = b.httpsResolved[host]
	case RouteKindGRPC:
		routes = b.grpcResolved[host]
	case RouteKindTLSTerminate, RouteKindTLSPassthrough:
		routes = b.tlsResolved[host]
	default:
		return nil, false
	}

	selected, ok := selectK8sRoute(routes, requestPath, kind)
	if !ok {
		return nil, false
	}
	return materializedToResolved(selected), true
}

func (b *K8sBridge) resolvePort(kind RouteKind, port uint32) (*K8sResolvedBackend, bool) {
	b.mu.RLock()
	defer b.mu.RUnlock()

	var routes []k8sMaterializedRoute
	switch kind {
	case RouteKindTCP:
		routes = b.tcpResolved[port]
	case RouteKindUDP:
		routes = b.udpResolved[port]
	default:
		return nil, false
	}
	if len(routes) == 0 {
		return nil, false
	}
	return materializedToResolved(routes[0]), true
}

func materializedToResolved(route k8sMaterializedRoute) *K8sResolvedBackend {
	return &K8sResolvedBackend{
		Kind:     route.kind,
		Hostname: route.hostname,
		Port:     route.port,
		Binding:  cloneBinding(route.binding),
		Route:    cloneRoute(route.route),
	}
}

func (b *K8sBridge) rebuildRoutesLocked() {
	b.httpResolved = make(map[string][]k8sMaterializedRoute)
	b.httpsResolved = make(map[string][]k8sMaterializedRoute)
	b.grpcResolved = make(map[string][]k8sMaterializedRoute)
	b.tlsResolved = make(map[string][]k8sMaterializedRoute)
	b.tcpResolved = make(map[uint32][]k8sMaterializedRoute)
	b.udpResolved = make(map[uint32][]k8sMaterializedRoute)

	b.rebuildIngressRoutesLocked()
	b.rebuildGatewayRoutesLocked()
}

func normalizeIngressPath(path string) string {
	path = strings.TrimSpace(path)
	if path == "" || path[0] != '/' {
		return "/"
	}
	return path
}

func selectK8sRoute(routes []k8sMaterializedRoute, requestPath string, kind RouteKind) (k8sMaterializedRoute, bool) {
	var (
		selected k8sMaterializedRoute
		found    bool
	)
	for _, candidate := range routes {
		if candidate.kind != kind {
			continue
		}
		if !k8sPathMatches(candidate.pathKind, candidate.path, requestPath) {
			continue
		}
		if !found || compareK8sRoutePriority(candidate, selected) > 0 {
			selected = candidate
			found = true
		}
	}
	return selected, found
}

func compareK8sRoutePriority(left, right k8sMaterializedRoute) int {
	if left.pathKind != right.pathKind {
		if left.pathKind == k8sRoutePathExact {
			return 1
		}
		if right.pathKind == k8sRoutePathExact {
			return -1
		}
	}
	if len(left.path) != len(right.path) {
		if len(left.path) > len(right.path) {
			return 1
		}
		return -1
	}
	if left.routeOrder != right.routeOrder {
		if left.routeOrder < right.routeOrder {
			return 1
		}
		return -1
	}
	if left.binding != nil && right.binding != nil && left.binding.ID != right.binding.ID {
		if left.binding.ID > right.binding.ID {
			return 1
		}
		return -1
	}
	return 0
}

func k8sPathMatches(pathKind k8sRoutePathKind, routePath, requestPath string) bool {
	routePath = normalizeIngressPath(routePath)
	requestPath = normalizeIngressPath(requestPath)

	switch pathKind {
	case k8sRoutePathExact:
		return requestPath == routePath
	case k8sRoutePathPrefix:
		if routePath == "/" {
			return true
		}
		if requestPath == routePath {
			return true
		}
		if !strings.HasPrefix(requestPath, routePath) {
			return false
		}
		return strings.HasPrefix(requestPath[len(routePath):], "/")
	default:
		return false
	}
}
