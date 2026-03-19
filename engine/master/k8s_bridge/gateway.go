package k8s_bridge

import (
	"context"
	"errors"
	"fmt"
	"strings"
	"sync"
	"time"

	enginepkg "github.com/jabberwocky238/luna-edge/engine"
	"github.com/jabberwocky238/luna-edge/repository"
	"github.com/jabberwocky238/luna-edge/repository/metadata"
	"gorm.io/gorm"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/apis/meta/v1/unstructured"
	"k8s.io/apimachinery/pkg/runtime/schema"
	"k8s.io/client-go/dynamic"
	dynamicinformer "k8s.io/client-go/dynamic/dynamicinformer"
	"k8s.io/client-go/rest"
	"k8s.io/client-go/tools/cache"
)

var (
	gatewayGVR   = schema.GroupVersionResource{Group: "gateway.networking.k8s.io", Version: "v1", Resource: "gateways"}
	httpRouteGVR = schema.GroupVersionResource{Group: "gateway.networking.k8s.io", Version: "v1", Resource: "httproutes"}
	tlsRouteGVR  = schema.GroupVersionResource{Group: "gateway.networking.k8s.io", Version: "v1alpha2", Resource: "tlsroutes"}
)

// GatewayBridge 预留给 Gateway API 控制面桥。
// 当前先保留生命周期与共享按域名写库逻辑，后续再补完整监听与物化。
type GatewayBridge struct {
	namespace     string
	dynamicClient dynamic.Interface
	factory       dynamicinformer.DynamicSharedInformerFactory
	OnUpdate      func(ctx context.Context, fqdn string) error
	stopCh        chan struct{}
	ctx           context.Context
	repo          repository.Repository
	mu            sync.RWMutex
	gateways      map[string]*gatewayState
	httpRoutes    map[string]*httpRouteState
	tlsRoutes     map[string]*tlsRouteState
}

func NewGatewayBridge(namespace string, repo repository.Repository, OnUpdate func(ctx context.Context, fqdn string) error) (*GatewayBridge, error) {
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
	client, err := dynamic.NewForConfig(cfg)
	if err != nil {
		return nil, fmt.Errorf("create dynamic k8s client: %w", err)
	}
	return NewGatewayBridgeWithClient(namespace, client, repo, OnUpdate), nil
}

func NewGatewayBridgeWithClient(namespace string, dynamicClient dynamic.Interface, repo repository.Repository, OnUpdate func(ctx context.Context, fqdn string) error) *GatewayBridge {
	if namespace == "" {
		namespace = enginepkg.POD_NAMESPACE
	}
	if namespace == "" {
		namespace = "default"
	}
	bridge := &GatewayBridge{
		namespace:     namespace,
		dynamicClient: dynamicClient,
		OnUpdate:      OnUpdate,
		stopCh:        make(chan struct{}),
		repo:          repo,
		gateways:      map[string]*gatewayState{},
		httpRoutes:    map[string]*httpRouteState{},
		tlsRoutes:     map[string]*tlsRouteState{},
	}
	bridge.ensureInformer()
	return bridge
}

func (b *GatewayBridge) LoadInitial(ctx context.Context) error {
	if b == nil || b.dynamicClient == nil {
		return nil
	}
	if err := b.loadGateways(ctx); err != nil {
		return err
	}
	if err := b.loadHTTPRoutes(ctx); err != nil {
		return err
	}
	if err := b.loadTLSRoutes(ctx); err != nil {
		return err
	}
	return b.syncHosts(ctx, b.collectHosts(), nil)
}

func (b *GatewayBridge) Listen(ctx context.Context) {
	if b == nil || b.factory == nil {
		return
	}
	if ctx == nil {
		return
	}
	b.ctx = ctx
	b.factory.Start(b.stopCh)
	go func() {
		<-ctx.Done()
		_ = b.Stop()
	}()
}

func (b *GatewayBridge) Stop() error {
	if b == nil {
		return nil
	}
	select {
	case <-b.stopCh:
		return nil
	default:
		close(b.stopCh)
		return nil
	}
}

func (b *GatewayBridge) ensureInformer() {
	if b == nil || b.dynamicClient == nil || b.factory != nil {
		return
	}
	b.factory = dynamicinformer.NewFilteredDynamicSharedInformerFactory(b.dynamicClient, 30*time.Second, b.namespace, nil)
	b.factory.ForResource(gatewayGVR).Informer().AddEventHandler(cache.ResourceEventHandlerFuncs{
		AddFunc:    func(obj interface{}) { b.storeGateway(asUnstructured(obj)) },
		UpdateFunc: func(_, newObj interface{}) { b.storeGateway(asUnstructured(newObj)) },
		DeleteFunc: b.deleteGateway,
	})
	b.factory.ForResource(httpRouteGVR).Informer().AddEventHandler(cache.ResourceEventHandlerFuncs{
		AddFunc:    func(obj interface{}) { b.storeHTTPRoute(asUnstructured(obj)) },
		UpdateFunc: func(_, newObj interface{}) { b.storeHTTPRoute(asUnstructured(newObj)) },
		DeleteFunc: b.deleteHTTPRoute,
	})
	b.factory.ForResource(tlsRouteGVR).Informer().AddEventHandler(cache.ResourceEventHandlerFuncs{
		AddFunc:    func(obj interface{}) { b.storeTLSRoute(asUnstructured(obj)) },
		UpdateFunc: func(_, newObj interface{}) { b.storeTLSRoute(asUnstructured(newObj)) },
		DeleteFunc: b.deleteTLSRoute,
	})
}

func (b *GatewayBridge) loadGateways(ctx context.Context) error {
	list, err := b.dynamicClient.Resource(gatewayGVR).Namespace(b.namespace).List(ctx, metav1.ListOptions{})
	if err != nil {
		return err
	}
	b.mu.Lock()
	defer b.mu.Unlock()
	b.gateways = map[string]*gatewayState{}
	for i := range list.Items {
		if state := parseGatewayState(list.Items[i].DeepCopy()); state != nil {
			b.gateways[state.key] = state
		}
	}
	return nil
}

func (b *GatewayBridge) storeGateway(obj *unstructured.Unstructured) {
	if obj == nil {
		return
	}
	b.mu.Lock()
	oldHosts := b.collectHostsLocked()
	var affected []string
	if state := parseGatewayState(obj); state != nil {
		b.gateways[state.key] = state
		affected = gatewayHosts(state)
	}
	newHosts := b.collectHostsLocked()
	b.mu.Unlock()
	_ = b.syncHosts(b.runtimeContext(), affected, diffStrings(oldHosts, newHosts))
}

func (b *GatewayBridge) deleteGateway(obj interface{}) {
	b.mu.Lock()
	oldHosts := b.collectHostsLocked()
	deleteByNamespaceName(obj, func(namespace, name string) { delete(b.gateways, namespace+"/"+name) })
	newHosts := b.collectHostsLocked()
	b.mu.Unlock()
	_ = b.syncHosts(b.runtimeContext(), nil, diffStrings(oldHosts, newHosts))
}

func (b *GatewayBridge) runtimeContext() context.Context {
	if b != nil && b.ctx != nil {
		return b.ctx
	}
	return context.Background()
}

func (b *GatewayBridge) collectHosts() []string {
	b.mu.RLock()
	defer b.mu.RUnlock()
	return b.collectHostsLocked()
}

func (b *GatewayBridge) collectHostsLocked() []string {
	set := map[string]struct{}{}
	for _, gw := range b.gateways {
		for _, listener := range gw.listeners {
			host := normalizeHost(listener.hostname)
			if host != "" {
				set[host] = struct{}{}
			}
		}
	}
	for _, route := range b.httpRoutes {
		for _, host := range route.hostnames {
			if normalized := normalizeHost(host); normalized != "" {
				set[normalized] = struct{}{}
			}
		}
	}
	for _, route := range b.tlsRoutes {
		for _, host := range route.hostnames {
			if normalized := normalizeHost(host); normalized != "" {
				set[normalized] = struct{}{}
			}
		}
	}
	return mapKeys(set)
}

func (b *GatewayBridge) syncHosts(ctx context.Context, affectedHosts, removedHosts []string) error {
	next := b.materializeByHost(affectedHosts)
	if err := syncDomainSet(ctx, b.repo, next, affectedHosts, removedHosts); err != nil {
		return err
	}
	for _, item := range removedHosts {
		if err := b.OnUpdate(ctx, item); err != nil {
			return err
		}
	}
	for _, item := range affectedHosts {
		if err := b.OnUpdate(ctx, item); err != nil {
			return err
		}
	}
	return nil
}

func (b *GatewayBridge) materializeByHost(hosts []string) map[string]domainMaterialized {
	hostSet := map[string]struct{}{}
	for _, host := range hosts {
		if normalized := normalizeHost(host); normalized != "" {
			hostSet[normalized] = struct{}{}
		}
	}
	out := map[string]domainMaterialized{}
	for _, route := range b.httpRoutes {
		for _, parent := range route.parents {
			gw := b.gateways[parent.gatewayKey]
			if gw == nil {
				continue
			}
			for _, listener := range gw.listeners {
				if parent.sectionName != "" && parent.sectionName != listener.name {
					continue
				}
				if listener.protocol != "HTTP" && listener.protocol != "HTTPS" {
					continue
				}
				hostsForRoute := route.hostnames
				if len(hostsForRoute) == 0 && listener.hostname != "" {
					hostsForRoute = []string{listener.hostname}
				}
				for idx, host := range hostsForRoute {
					host = normalizeHost(host)
					if _, ok := hostSet[host]; !ok || host == "" {
						continue
					}
					item := out[host]
					if item.domain.ID == "" {
						item.domain = metadata.DomainEndpoint{
							ID:          "k8s:domain:" + host,
							Hostname:    host,
							NeedCert:    listener.protocol == "HTTPS",
							BackendType: metadata.BackendTypeL7HTTP,
						}
						if listener.protocol == "HTTPS" {
							item.domain.BackendType = metadata.BackendTypeL7HTTPS
						}
					}
					if listener.protocol == "HTTPS" {
						item.domain.NeedCert = true
						switch item.domain.BackendType {
						case metadata.BackendTypeL7HTTP:
							item.domain.BackendType = metadata.BackendTypeL7HTTPBoth
						default:
							item.domain.BackendType = metadata.BackendTypeL7HTTPS
						}
					} else if listener.protocol == "HTTP" && item.domain.NeedCert {
						switch item.domain.BackendType {
						case metadata.BackendTypeL7HTTPS, metadata.BackendTypeL7HTTPBoth:
							item.domain.BackendType = metadata.BackendTypeL7HTTPBoth
						}
					}
					for ruleIdx, rule := range route.rules {
						backendID := fmt.Sprintf("k8s:backend:gateway:%s:%s:%s:%d:%d", route.namespace, route.name, host, idx, ruleIdx)
						item.backends = append(item.backends, metadata.ServiceBackendRef{
							ID:               backendID,
							ServiceNamespace: rule.backend.namespace,
							ServiceName:      rule.backend.name,
							ServicePort:      rule.backend.port,
						})
						priority := int32(len(normalizePath(rule.path)))
						if rule.exact {
							priority += 100000
						}
						item.routes = append(item.routes, metadata.HTTPRoute{
							ID:               fmt.Sprintf("k8s:route:gateway:%s:%s:%s:%d:%d", route.namespace, route.name, host, idx, ruleIdx),
							DomainEndpointID: item.domain.ID,
							Path:             normalizePath(rule.path),
							Priority:         priority,
							BackendRefID:     backendID,
						})
					}
					out[host] = item
				}
			}
		}
	}
	for _, route := range b.tlsRoutes {
		for _, parent := range route.parents {
			gw := b.gateways[parent.gatewayKey]
			if gw == nil {
				continue
			}
			for _, listener := range gw.listeners {
				if parent.sectionName != "" && parent.sectionName != listener.name {
					continue
				}
				if listener.protocol != "TLS" {
					continue
				}
				hostsForRoute := route.hostnames
				if len(hostsForRoute) == 0 && listener.hostname != "" {
					hostsForRoute = []string{listener.hostname}
				}
				for idx, host := range hostsForRoute {
					host = normalizeHost(host)
					if _, ok := hostSet[host]; !ok || host == "" {
						continue
					}
					item := out[host]
					backendID := fmt.Sprintf("k8s:backend:gateway-tls:%s:%s:%s:%d", route.namespace, route.name, host, idx)
					item.domain = metadata.DomainEndpoint{
						ID:              "k8s:domain:" + host,
						Hostname:        host,
						NeedCert:        !listener.passthrough,
						BindedServiceID: backendID,
						BackendType:     metadata.BackendTypeL4TLSPassthrough,
					}
					if !listener.passthrough {
						item.domain.BackendType = metadata.BackendTypeL4TLSTermination
					}
					item.backends = []metadata.ServiceBackendRef{{
						ID:               backendID,
						ServiceNamespace: route.backend.namespace,
						ServiceName:      route.backend.name,
						ServicePort:      route.backend.port,
					}}
					item.routes = nil
					out[host] = item
				}
			}
		}
	}
	for host, item := range out {
		item.backends = dedupeServiceBackendRefs(item.backends)
		item.routes = dedupeHTTPRoutes(item.routes)
		out[host] = item
	}
	return out
}

type gatewayState struct {
	key       string
	listeners []gatewayListener
}

type gatewayListener struct {
	name        string
	protocol    string
	hostname    string
	passthrough bool
}

type parentRef struct {
	gatewayKey  string
	sectionName string
}

type backendRef struct {
	namespace string
	name      string
	port      uint32
}

func gatewayHosts(state *gatewayState) []string {
	if state == nil {
		return nil
	}
	var hosts []string
	for _, listener := range state.listeners {
		if normalized := normalizeHost(listener.hostname); normalized != "" {
			hosts = append(hosts, normalized)
		}
	}
	return hosts
}

func normalizeHosts(values []string) []string {
	var out []string
	for _, value := range values {
		if normalized := normalizeHost(value); normalized != "" {
			out = append(out, normalized)
		}
	}
	return out
}

func parseGatewayState(obj *unstructured.Unstructured) *gatewayState {
	if obj == nil {
		return nil
	}
	rawListeners, _, _ := unstructured.NestedSlice(obj.Object, "spec", "listeners")
	state := &gatewayState{key: obj.GetNamespace() + "/" + obj.GetName()}
	for _, raw := range rawListeners {
		item, ok := raw.(map[string]interface{})
		if !ok {
			continue
		}
		name, _, _ := unstructured.NestedString(item, "name")
		protocol, _, _ := unstructured.NestedString(item, "protocol")
		hostname, _, _ := unstructured.NestedString(item, "hostname")
		tlsMode, _, _ := unstructured.NestedString(item, "tls", "mode")
		state.listeners = append(state.listeners, gatewayListener{
			name:        name,
			protocol:    strings.ToUpper(strings.TrimSpace(protocol)),
			hostname:    hostname,
			passthrough: strings.EqualFold(strings.TrimSpace(tlsMode), "Passthrough"),
		})
	}
	return state
}

func nestedStringSlice(obj map[string]interface{}, fields ...string) []string {
	values, _, _ := unstructured.NestedStringSlice(obj, fields...)
	return values
}

func diffStrings(oldValues, newValues []string) []string {
	newSet := map[string]struct{}{}
	for _, value := range newValues {
		newSet[value] = struct{}{}
	}
	var out []string
	for _, value := range oldValues {
		if _, ok := newSet[value]; ok {
			continue
		}
		out = append(out, value)
	}
	return out
}

func routeHostsFromDeletedObject(obj interface{}) []string {
	item, ok := obj.(*unstructured.Unstructured)
	if !ok || item == nil {
		return nil
	}
	var hosts []string
	for _, host := range nestedStringSlice(item.Object, "spec", "hostnames") {
		if normalized := normalizeHost(host); normalized != "" {
			hosts = append(hosts, normalized)
		}
	}
	return hosts
}

type domainMaterialized struct {
	domain   metadata.DomainEndpoint
	backends []metadata.ServiceBackendRef
	routes   []metadata.HTTPRoute
}

type certificateDesiredMarker interface {
	MarkCertificateDesired(ctx context.Context, hostname string)
}

func syncDomainSet(ctx context.Context, repo repository.Repository, next map[string]domainMaterialized, affectedHosts []string, removedHosts []string) error {
	if repo == nil {
		return nil
	}
	if err := syncDomainSetOnce(ctx, repo, next, affectedHosts, removedHosts); err != nil {
		return err
	}
	return nil
}

func syncDomainSetOnce(ctx context.Context, repo repository.Repository, next map[string]domainMaterialized, affectedHosts []string, removedHosts []string) error {
	seen := map[string]struct{}{}
	for _, host := range affectedHosts {
		host = normalizeHost(host)
		if host == "" {
			continue
		}
		if _, ok := seen[host]; ok {
			continue
		}
		seen[host] = struct{}{}
		item, ok := next[host]
		if !ok {
			if err := deleteManagedDomain(ctx, repo, host); err != nil {
				return err
			}
			continue
		}
		changed, err := upsertManagedDomain(ctx, repo, item)
		if err != nil {
			return err
		}
		if changed && item.domain.NeedCert {
			if marker, ok := repo.(certificateDesiredMarker); ok {
				marker.MarkCertificateDesired(ctx, item.domain.Hostname)
			}
		}
	}
	for _, host := range removedHosts {
		host = normalizeHost(host)
		if host == "" {
			continue
		}
		if _, ok := seen[host]; ok {
			continue
		}
		seen[host] = struct{}{}
		if err := deleteManagedDomain(ctx, repo, host); err != nil {
			return err
		}
	}
	return nil
}

func upsertManagedDomain(ctx context.Context, repo repository.Repository, item domainMaterialized) (bool, error) {
	existing, err := repo.GetDomainEndpointByID(ctx, item.domain.ID)
	if err != nil && !errors.Is(err, gorm.ErrRecordNotFound) {
		return false, err
	}
	currentRoutes, err := repo.ListHTTPRoutesByDomainID(ctx, item.domain.ID)
	if err != nil && !errors.Is(err, gorm.ErrRecordNotFound) {
		return false, err
	}
	currentBackends, err := repo.ListServiceBindingsByDomainID(ctx, item.domain.ID)
	if err != nil && !errors.Is(err, gorm.ErrRecordNotFound) {
		return false, err
	}
	if managedDomainUnchanged(existing, currentRoutes, currentBackends, item) {
		return false, nil
	}
	if err := repo.DomainEndpoints().UpsertResource(ctx, &item.domain); err != nil {
		return false, err
	}
	currentRouteIDs := make(map[string]metadata.HTTPRoute, len(currentRoutes))
	for i := range currentRoutes {
		currentRouteIDs[currentRoutes[i].ID] = currentRoutes[i]
	}
	nextRouteIDs := map[string]struct{}{}
	nextBackendIDs := map[string]struct{}{}
	for i := range item.backends {
		nextBackendIDs[item.backends[i].ID] = struct{}{}
		if err := repo.ServiceBindingRefs().UpsertResource(ctx, &item.backends[i]); err != nil {
			return false, err
		}
	}
	for i := range item.routes {
		nextRouteIDs[item.routes[i].ID] = struct{}{}
		if err := repo.HTTPRoutes().UpsertResource(ctx, &item.routes[i]); err != nil {
			return false, err
		}
	}
	for id, route := range currentRouteIDs {
		if _, ok := nextRouteIDs[id]; ok {
			continue
		}
		if err := repo.HTTPRoutes().DeleteResourceByField(ctx, &metadata.HTTPRoute{}, "id", id); err != nil {
			return false, err
		}
		if strings.HasPrefix(route.BackendRefID, "k8s:backend:") {
			if _, keep := nextBackendIDs[route.BackendRefID]; !keep {
				if err := repo.ServiceBindingRefs().DeleteResourceByField(ctx, &metadata.ServiceBackendRef{}, "id", route.BackendRefID); err != nil {
					return false, err
				}
			}
		}
	}
	if existing != nil && existing.BindedServiceID != "" && strings.HasPrefix(existing.BindedServiceID, "k8s:backend:") {
		if _, keep := nextBackendIDs[existing.BindedServiceID]; !keep {
			if err := repo.ServiceBindingRefs().DeleteResourceByField(ctx, &metadata.ServiceBackendRef{}, "id", existing.BindedServiceID); err != nil {
				return false, err
			}
		}
	}
	return true, nil
}

func deleteManagedDomain(ctx context.Context, repo repository.Repository, hostname string) error {
	domain, err := repo.GetDomainEndpointByHostname(ctx, hostname)
	if err != nil {
		if errors.Is(err, gorm.ErrRecordNotFound) {
			return nil
		}
		return err
	}
	if domain == nil || !strings.HasPrefix(domain.ID, "k8s:domain:") {
		return nil
	}
	routes, err := repo.ListHTTPRoutesByDomainID(ctx, domain.ID)
	if err == nil {
		for i := range routes {
			if err := repo.HTTPRoutes().DeleteResourceByField(ctx, &metadata.HTTPRoute{}, "id", routes[i].ID); err != nil {
				return err
			}
			if strings.HasPrefix(routes[i].BackendRefID, "k8s:backend:") {
				if err := repo.ServiceBindingRefs().DeleteResourceByField(ctx, &metadata.ServiceBackendRef{}, "id", routes[i].BackendRefID); err != nil {
					return err
				}
			}
		}
	}
	if domain.BindedServiceID != "" && strings.HasPrefix(domain.BindedServiceID, "k8s:backend:") {
		if err := repo.ServiceBindingRefs().DeleteResourceByField(ctx, &metadata.ServiceBackendRef{}, "id", domain.BindedServiceID); err != nil {
			return err
		}
	}
	return repo.DomainEndpoints().DeleteResourceByField(ctx, &metadata.DomainEndpoint{}, "id", domain.ID)
}

func managedDomainUnchanged(existing *metadata.DomainEndpoint, currentRoutes []metadata.HTTPRoute, currentBackends []metadata.ServiceBackendRef, item domainMaterialized) bool {
	if existing == nil {
		return false
	}
	if existing.Hostname != item.domain.Hostname || existing.NeedCert != item.domain.NeedCert || existing.BackendType != item.domain.BackendType || existing.BindedServiceID != item.domain.BindedServiceID {
		return false
	}
	if len(currentRoutes) != len(item.routes) || len(currentBackends) != len(item.backends) {
		return false
	}
	currentRouteMap := make(map[string]metadata.HTTPRoute, len(currentRoutes))
	for i := range currentRoutes {
		currentRouteMap[currentRoutes[i].ID] = currentRoutes[i]
	}
	for i := range item.routes {
		current, ok := currentRouteMap[item.routes[i].ID]
		if !ok || current.DomainEndpointID != item.routes[i].DomainEndpointID || current.Path != item.routes[i].Path || current.Priority != item.routes[i].Priority || current.BackendRefID != item.routes[i].BackendRefID {
			return false
		}
	}
	currentBackendMap := make(map[string]metadata.ServiceBackendRef, len(currentBackends))
	for i := range currentBackends {
		currentBackendMap[currentBackends[i].ID] = currentBackends[i]
	}
	for i := range item.backends {
		current, ok := currentBackendMap[item.backends[i].ID]
		if !ok || current.ServiceNamespace != item.backends[i].ServiceNamespace || current.ServiceName != item.backends[i].ServiceName || current.ServicePort != item.backends[i].ServicePort {
			return false
		}
	}
	return true
}

func normalizeHost(host string) string {
	host = strings.TrimSpace(strings.ToLower(host))
	host = strings.TrimPrefix(host, "http://")
	host = strings.TrimPrefix(host, "https://")
	if idx := strings.Index(host, ":"); idx >= 0 {
		host = host[:idx]
	}
	return strings.TrimSuffix(host, ".")
}

func normalizePath(path string) string {
	path = strings.TrimSpace(path)
	if path == "" || path[0] != '/' {
		return "/"
	}
	return path
}

func dedupeServiceBackendRefs(in []metadata.ServiceBackendRef) []metadata.ServiceBackendRef {
	if len(in) < 2 {
		return in
	}
	out := make([]metadata.ServiceBackendRef, 0, len(in))
	seen := make(map[string]struct{}, len(in))
	for i := range in {
		if _, ok := seen[in[i].ID]; ok {
			continue
		}
		seen[in[i].ID] = struct{}{}
		out = append(out, in[i])
	}
	return out
}

func dedupeHTTPRoutes(in []metadata.HTTPRoute) []metadata.HTTPRoute {
	if len(in) < 2 {
		return in
	}
	out := make([]metadata.HTTPRoute, 0, len(in))
	seen := make(map[string]struct{}, len(in))
	for i := range in {
		if _, ok := seen[in[i].ID]; ok {
			continue
		}
		seen[in[i].ID] = struct{}{}
		out = append(out, in[i])
	}
	return out
}
