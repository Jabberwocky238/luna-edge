package ingress

import (
	"context"
	"encoding/json"
	"fmt"
	"strings"
	"time"

	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/apis/meta/v1/unstructured"
	"k8s.io/apimachinery/pkg/runtime/schema"
	dynamicinformer "k8s.io/client-go/dynamic/dynamicinformer"
	"k8s.io/client-go/tools/cache"
)

var (
	gatewayGVR   = schema.GroupVersionResource{Group: "gateway.networking.k8s.io", Version: "v1", Resource: "gateways"}
	httpRouteGVR = schema.GroupVersionResource{Group: "gateway.networking.k8s.io", Version: "v1", Resource: "httproutes"}
	grpcRouteGVR = schema.GroupVersionResource{Group: "gateway.networking.k8s.io", Version: "v1", Resource: "grpcroutes"}
	tlsRouteGVR  = schema.GroupVersionResource{Group: "gateway.networking.k8s.io", Version: "v1alpha2", Resource: "tlsroutes"}
	tcpRouteGVR  = schema.GroupVersionResource{Group: "gateway.networking.k8s.io", Version: "v1alpha2", Resource: "tcproutes"}
	udpRouteGVR  = schema.GroupVersionResource{Group: "gateway.networking.k8s.io", Version: "v1alpha2", Resource: "udproutes"}
)

type k8sGatewayState struct {
	name      string
	namespace string
	listeners map[string]k8sGatewayListenerState
}

type k8sGatewayListenerState struct {
	name      string
	protocol  string
	hostname  string
	port      uint32
	tlsMode   string
	routeKind RouteKind
}

type k8sBackendRef struct {
	namespace string
	name      string
	port      uint32
}

type k8sHTTPRouteState struct {
	name       string
	namespace  string
	hostnames  []string
	parentRefs []k8sParentRef
	rules      []k8sHTTPRouteRuleState
}

type k8sHTTPRouteRuleState struct {
	path     string
	pathKind k8sRoutePathKind
	backend  k8sBackendRef
}

type k8sGRPCRouteState = k8sHTTPRouteState
type k8sTLSRouteState = k8sL4RouteState
type k8sTCPRouteState = k8sL4RouteState
type k8sUDPRouteState = k8sL4RouteState

type k8sL4RouteState struct {
	name       string
	namespace  string
	hostnames  []string
	parentRefs []k8sParentRef
	backend    k8sBackendRef
}

type k8sParentRef struct {
	gatewayKey  string
	sectionName string
}

func (b *K8sBridge) ensureGatewayInformers() {
	if b.dynamicClient == nil || b.gatewayFactory != nil {
		return
	}
	b.gatewayFactory = dynamicinformer.NewFilteredDynamicSharedInformerFactory(
		b.dynamicClient,
		30*time.Second,
		b.namespace,
		nil,
	)
	registerGatewayInformer := func(gvr schema.GroupVersionResource, add, update, del func(interface{})) {
		b.gatewayFactory.ForResource(gvr).Informer().AddEventHandler(cache.ResourceEventHandlerFuncs{
			AddFunc:    add,
			UpdateFunc: func(_, newObj interface{}) { update(newObj) },
			DeleteFunc: del,
		})
	}
	registerGatewayInformer(gatewayGVR, b.storeGateway, b.storeGateway, b.deleteGateway)
	registerGatewayInformer(httpRouteGVR, b.storeHTTPRoute, b.storeHTTPRoute, b.deleteHTTPRoute)
	registerGatewayInformer(grpcRouteGVR, b.storeGRPCRoute, b.storeGRPCRoute, b.deleteGRPCRoute)
	registerGatewayInformer(tlsRouteGVR, b.storeTLSRoute, b.storeTLSRoute, b.deleteTLSRoute)
	registerGatewayInformer(tcpRouteGVR, b.storeTCPRoute, b.storeTCPRoute, b.deleteTCPRoute)
	registerGatewayInformer(udpRouteGVR, b.storeUDPRoute, b.storeUDPRoute, b.deleteUDPRoute)
}

func (b *K8sBridge) loadInitialGateways(ctx context.Context) error {
	if b.dynamicClient == nil {
		return nil
	}
	load := func(gvr schema.GroupVersionResource, store func(*unstructured.Unstructured)) error {
		list, err := b.dynamicClient.Resource(gvr).Namespace(b.namespace).List(ctx, metav1.ListOptions{})
		if err != nil {
			return nil
		}
		for i := range list.Items {
			obj := list.Items[i]
			store(obj.DeepCopy())
		}
		return nil
	}
	if err := load(gatewayGVR, b.storeGatewayUnstructured); err != nil {
		return fmt.Errorf("list gateways: %w", err)
	}
	if err := load(httpRouteGVR, b.storeHTTPRouteUnstructured); err != nil {
		return fmt.Errorf("list httproutes: %w", err)
	}
	if err := load(grpcRouteGVR, b.storeGRPCRouteUnstructured); err != nil {
		return fmt.Errorf("list grpcroutes: %w", err)
	}
	if err := load(tlsRouteGVR, b.storeTLSRouteUnstructured); err != nil {
		return fmt.Errorf("list tlsroutes: %w", err)
	}
	if err := load(tcpRouteGVR, b.storeTCPRouteUnstructured); err != nil {
		return fmt.Errorf("list tcproutes: %w", err)
	}
	if err := load(udpRouteGVR, b.storeUDPRouteUnstructured); err != nil {
		return fmt.Errorf("list udproutes: %w", err)
	}
	return nil
}

func (b *K8sBridge) rebuildGatewayRoutesLocked() {
	for _, route := range b.httpRoutes {
		b.materializeHTTPFamilyLocked(route.namespace, route.name, route.hostnames, route.parentRefs, route.rules)
	}
	for _, route := range b.grpcRoutes {
		b.materializeGRPCFamilyLocked(route.namespace, route.name, route.hostnames, route.parentRefs, route.rules)
	}
	for _, route := range b.tlsRoutes {
		b.materializeL4Locked(RouteKindTLSTerminate, route)
	}
	for _, route := range b.tcpRoutes {
		b.materializeL4Locked(RouteKindTCP, route)
	}
	for _, route := range b.udpRoutes {
		b.materializeL4Locked(RouteKindUDP, route)
	}
}

func (b *K8sBridge) materializeHTTPFamilyLocked(namespace, routeName string, hostnames []string, parentRefs []k8sParentRef, rules []k8sHTTPRouteRuleState) {
	for _, parentRef := range parentRefs {
		gateway := b.gateways[parentRef.gatewayKey]
		if gateway == nil {
			continue
		}
		for _, listener := range gateway.listeners {
			if parentRef.sectionName != "" && listener.name != parentRef.sectionName {
				continue
			}
			if listener.routeKind != RouteKindHTTP && listener.routeKind != RouteKindHTTPS {
				continue
			}
			emittedKind := listener.routeKind
			for _, host := range effectiveHosts(hostnames, listener.hostname) {
				for idx, rule := range rules {
					routeJSON, _ := json.Marshal(rule)
					materialized := b.newMaterializedRoute(emittedKind, namespace, routeName, host, listener.port, rule.backend, routeJSON, idx)
					materialized.path = rule.path
					materialized.pathKind = rule.pathKind
					materialized.listener = listener.name
					switch emittedKind {
					case RouteKindHTTP:
						b.httpResolved[host] = append(b.httpResolved[host], materialized)
					case RouteKindHTTPS:
						b.httpsResolved[host] = append(b.httpsResolved[host], materialized)
					case RouteKindGRPC:
						b.grpcResolved[host] = append(b.grpcResolved[host], materialized)
					}
				}
			}
		}
	}
}

func (b *K8sBridge) materializeGRPCFamilyLocked(namespace, routeName string, hostnames []string, parentRefs []k8sParentRef, rules []k8sHTTPRouteRuleState) {
	for _, parentRef := range parentRefs {
		gateway := b.gateways[parentRef.gatewayKey]
		if gateway == nil {
			continue
		}
		for _, listener := range gateway.listeners {
			if parentRef.sectionName != "" && listener.name != parentRef.sectionName {
				continue
			}
			if !listenerAllowsKind(listener, RouteKindGRPC) {
				continue
			}
			for _, host := range effectiveHosts(hostnames, listener.hostname) {
				for idx, rule := range rules {
					routeJSON, _ := json.Marshal(rule)
					materialized := b.newMaterializedRoute(RouteKindGRPC, namespace, routeName, host, listener.port, rule.backend, routeJSON, idx)
					materialized.path = rule.path
					materialized.pathKind = rule.pathKind
					materialized.listener = listener.name
					b.grpcResolved[host] = append(b.grpcResolved[host], materialized)
				}
			}
		}
	}
}

func (b *K8sBridge) materializeL4Locked(kind RouteKind, route *k8sL4RouteState) {
	for _, parentRef := range route.parentRefs {
		gateway := b.gateways[parentRef.gatewayKey]
		if gateway == nil {
			continue
		}
		for _, listener := range gateway.listeners {
			if parentRef.sectionName != "" && listener.name != parentRef.sectionName {
				continue
			}
			if !listenerAllowsKind(listener, kind) {
				continue
			}
			hosts := effectiveHosts(route.hostnames, listener.hostname)
			if len(hosts) == 0 {
				hosts = []string{listener.hostname}
			}
			for _, host := range hosts {
				routeJSON, _ := json.Marshal(route.backend)
				materialized := b.newMaterializedRoute(kind, route.namespace, route.name, host, listener.port, route.backend, routeJSON, 0)
				materialized.listener = listener.name
				if kind == RouteKindTLSTerminate && strings.EqualFold(listener.tlsMode, "Passthrough") {
					materialized.kind = RouteKindTLSPassthrough
				}
				switch materialized.kind {
				case RouteKindTLSTerminate, RouteKindTLSPassthrough:
					if host != "" {
						b.tlsResolved[host] = append(b.tlsResolved[host], materialized)
					}
				case RouteKindTCP:
					b.tcpResolved[listener.port] = append(b.tcpResolved[listener.port], materialized)
				case RouteKindUDP:
					b.udpResolved[listener.port] = append(b.udpResolved[listener.port], materialized)
				}
			}
		}
	}
}

func (b *K8sBridge) newMaterializedRoute(kind RouteKind, namespace, routeName, host string, port uint32, backend k8sBackendRef, raw []byte, order int) k8sMaterializedRoute {
	bindingID := fmt.Sprintf("k8s:%s:%s:%s:%s:%d", kind, namespace, routeName, backend.name, order)
	backendProtocol := backendProtocolForRouteKind(kind)
	return k8sMaterializedRoute{
		kind:     kind,
		hostname: host,
		port:     port,
		path:     "/",
		pathKind: k8sRoutePathPrefix,
		binding: &BackendBinding{
			ID:           bindingID,
			Hostname:     host,
			ServiceID:    fmt.Sprintf("%s/%s", backend.namespace, backend.name),
			Namespace:    backend.namespace,
			Name:         backend.name,
			Address:      buildServiceAddress(backend.name, backend.namespace),
			Port:         backend.port,
			Protocol:     backendProtocol,
			RouteVersion: 1,
			BackendJSON:  string(raw),
		},
		route: &ResolvedRoute{
			Hostname:     host,
			RouteVersion: 1,
			Protocol:     kind,
			RouteJSON:    string(raw),
			BindingID:    bindingID,
		},
		routeOrder: order,
	}
}

func backendProtocolForRouteKind(kind RouteKind) RouteKind {
	switch kind {
	case RouteKindHTTPS:
		return RouteKindHTTP
	default:
		return kind
	}
}

func listenerAllowsKind(listener k8sGatewayListenerState, kind RouteKind) bool {
	switch kind {
	case RouteKindHTTP:
		return listener.routeKind == RouteKindHTTP
	case RouteKindHTTPS:
		return listener.routeKind == RouteKindHTTPS
	case RouteKindGRPC:
		return listener.routeKind == RouteKindGRPC || listener.routeKind == RouteKindHTTP
	case RouteKindTLSTerminate, RouteKindTLSPassthrough:
		return listener.routeKind == RouteKindTLSTerminate
	case RouteKindTCP:
		return listener.routeKind == RouteKindTCP
	case RouteKindUDP:
		return listener.routeKind == RouteKindUDP
	default:
		return false
	}
}

func effectiveHosts(routeHosts []string, listenerHost string) []string {
	if len(routeHosts) > 0 {
		out := make([]string, 0, len(routeHosts))
		for _, host := range routeHosts {
			if normalized := normalizeHost(host); normalized != "" {
				out = append(out, normalized)
			}
		}
		return out
	}
	if normalized := normalizeHost(listenerHost); normalized != "" {
		return []string{normalized}
	}
	return nil
}

func (b *K8sBridge) storeGateway(obj interface{}) { b.storeGatewayUnstructured(asUnstructured(obj)) }
func (b *K8sBridge) storeHTTPRoute(obj interface{}) {
	b.storeHTTPRouteUnstructured(asUnstructured(obj))
}
func (b *K8sBridge) storeGRPCRoute(obj interface{}) {
	b.storeGRPCRouteUnstructured(asUnstructured(obj))
}
func (b *K8sBridge) storeTLSRoute(obj interface{}) { b.storeTLSRouteUnstructured(asUnstructured(obj)) }
func (b *K8sBridge) storeTCPRoute(obj interface{}) { b.storeTCPRouteUnstructured(asUnstructured(obj)) }
func (b *K8sBridge) storeUDPRoute(obj interface{}) { b.storeUDPRouteUnstructured(asUnstructured(obj)) }

func (b *K8sBridge) deleteGateway(obj interface{}) { b.deleteGatewayKey(obj) }
func (b *K8sBridge) deleteHTTPRoute(obj interface{}) {
	b.deleteRouteKey(obj, func(key string) { delete(b.httpRoutes, key) })
}
func (b *K8sBridge) deleteGRPCRoute(obj interface{}) {
	b.deleteRouteKey(obj, func(key string) { delete(b.grpcRoutes, key) })
}
func (b *K8sBridge) deleteTLSRoute(obj interface{}) {
	b.deleteRouteKey(obj, func(key string) { delete(b.tlsRoutes, key) })
}
func (b *K8sBridge) deleteTCPRoute(obj interface{}) {
	b.deleteRouteKey(obj, func(key string) { delete(b.tcpRoutes, key) })
}
func (b *K8sBridge) deleteUDPRoute(obj interface{}) {
	b.deleteRouteKey(obj, func(key string) { delete(b.udpRoutes, key) })
}

func (b *K8sBridge) deleteGatewayKey(obj interface{}) {
	deleteByNamespaceName(obj, func(namespace, name string) {
		b.mu.Lock()
		delete(b.gateways, namespace+"/"+name)
		b.rebuildRoutesLocked()
		b.mu.Unlock()
	})
}

func (b *K8sBridge) deleteRouteKey(obj interface{}, deleter func(key string)) {
	deleteByNamespaceName(obj, func(namespace, name string) {
		b.mu.Lock()
		deleter(namespace + "/" + name)
		b.rebuildRoutesLocked()
		b.mu.Unlock()
	})
}

func (b *K8sBridge) storeGatewayUnstructured(obj *unstructured.Unstructured) {
	if obj == nil {
		return
	}
	state := parseGatewayState(obj)
	if state == nil {
		return
	}
	b.mu.Lock()
	b.gateways[state.namespace+"/"+state.name] = state
	b.rebuildRoutesLocked()
	b.mu.Unlock()
}

func (b *K8sBridge) storeHTTPRouteUnstructured(obj *unstructured.Unstructured) {
	if state := parseHTTPRouteState(obj); state != nil {
		b.mu.Lock()
		b.httpRoutes[state.namespace+"/"+state.name] = state
		b.rebuildRoutesLocked()
		b.mu.Unlock()
	}
}

func (b *K8sBridge) storeGRPCRouteUnstructured(obj *unstructured.Unstructured) {
	if state := parseHTTPRouteState(obj); state != nil {
		b.mu.Lock()
		b.grpcRoutes[state.namespace+"/"+state.name] = (*k8sGRPCRouteState)(state)
		b.rebuildRoutesLocked()
		b.mu.Unlock()
	}
}

func (b *K8sBridge) storeTLSRouteUnstructured(obj *unstructured.Unstructured) {
	if state := parseL4RouteState(obj); state != nil {
		b.mu.Lock()
		b.tlsRoutes[state.namespace+"/"+state.name] = (*k8sTLSRouteState)(state)
		b.rebuildRoutesLocked()
		b.mu.Unlock()
	}
}

func (b *K8sBridge) storeTCPRouteUnstructured(obj *unstructured.Unstructured) {
	if state := parseL4RouteState(obj); state != nil {
		b.mu.Lock()
		b.tcpRoutes[state.namespace+"/"+state.name] = (*k8sTCPRouteState)(state)
		b.rebuildRoutesLocked()
		b.mu.Unlock()
	}
}

func (b *K8sBridge) storeUDPRouteUnstructured(obj *unstructured.Unstructured) {
	if state := parseL4RouteState(obj); state != nil {
		b.mu.Lock()
		b.udpRoutes[state.namespace+"/"+state.name] = (*k8sUDPRouteState)(state)
		b.rebuildRoutesLocked()
		b.mu.Unlock()
	}
}

func asUnstructured(obj interface{}) *unstructured.Unstructured {
	value, ok := obj.(*unstructured.Unstructured)
	if ok {
		return value
	}
	return nil
}

func parseGatewayState(obj *unstructured.Unstructured) *k8sGatewayState {
	if obj == nil {
		return nil
	}
	listeners, _, _ := unstructured.NestedSlice(obj.Object, "spec", "listeners")
	state := &k8sGatewayState{
		name:      obj.GetName(),
		namespace: obj.GetNamespace(),
		listeners: make(map[string]k8sGatewayListenerState),
	}
	for _, raw := range listeners {
		item, ok := raw.(map[string]interface{})
		if !ok {
			continue
		}
		name, _, _ := unstructured.NestedString(item, "name")
		if name == "" {
			continue
		}
		protocol, _, _ := unstructured.NestedString(item, "protocol")
		hostname, _, _ := unstructured.NestedString(item, "hostname")
		port64, _, _ := unstructured.NestedInt64(item, "port")
		tlsMode, _, _ := unstructured.NestedString(item, "tls", "mode")
		state.listeners[name] = k8sGatewayListenerState{
			name:      name,
			protocol:  strings.ToUpper(strings.TrimSpace(protocol)),
			hostname:  hostname,
			port:      uint32(port64),
			tlsMode:   strings.TrimSpace(tlsMode),
			routeKind: routeKindFromListener(name, protocol, tlsMode),
		}
	}
	return state
}

func routeKindFromListener(name, protocol, tlsMode string) RouteKind {
	switch strings.ToLower(strings.TrimSpace(name)) {
	case "web":
		return RouteKindHTTP
	case "websecure":
		return RouteKindHTTPS
	}
	switch strings.ToUpper(strings.TrimSpace(protocol)) {
	case "HTTP":
		return RouteKindHTTP
	case "HTTPS":
		return RouteKindHTTPS
	case "GRPC", "HTTPS+GRPC":
		return RouteKindGRPC
	case "TLS":
		return RouteKindTLSTerminate
	case "TCP":
		return RouteKindTCP
	case "UDP":
		return RouteKindUDP
	default:
		return RouteKindHTTP
	}
}

func parseHTTPRouteState(obj *unstructured.Unstructured) *k8sHTTPRouteState {
	if obj == nil {
		return nil
	}
	state := &k8sHTTPRouteState{
		name:       obj.GetName(),
		namespace:  obj.GetNamespace(),
		hostnames:  nestedStringSlice(obj.Object, "spec", "hostnames"),
		parentRefs: parseParentRefs(obj.Object),
	}
	rules, _, _ := unstructured.NestedSlice(obj.Object, "spec", "rules")
	for _, raw := range rules {
		item, ok := raw.(map[string]interface{})
		if !ok {
			continue
		}
		path := "/"
		pathKind := k8sRoutePathPrefix
		matches, _, _ := unstructured.NestedSlice(item, "matches")
		if len(matches) > 0 {
			if match, ok := matches[0].(map[string]interface{}); ok {
				if matchPath, found, _ := unstructured.NestedString(match, "path", "value"); found && matchPath != "" {
					path = normalizeIngressPath(matchPath)
				}
				if matchType, found, _ := unstructured.NestedString(match, "path", "type"); found && strings.EqualFold(matchType, "Exact") {
					pathKind = k8sRoutePathExact
				}
			}
		}
		if backend, ok := parseFirstBackendRef(item, state.namespace); ok {
			state.rules = append(state.rules, k8sHTTPRouteRuleState{
				path:     path,
				pathKind: pathKind,
				backend:  backend,
			})
		}
	}
	return state
}

func parseL4RouteState(obj *unstructured.Unstructured) *k8sL4RouteState {
	if obj == nil {
		return nil
	}
	state := &k8sL4RouteState{
		name:       obj.GetName(),
		namespace:  obj.GetNamespace(),
		hostnames:  nestedStringSlice(obj.Object, "spec", "hostnames"),
		parentRefs: parseParentRefs(obj.Object),
	}
	rules, _, _ := unstructured.NestedSlice(obj.Object, "spec", "rules")
	for _, raw := range rules {
		item, ok := raw.(map[string]interface{})
		if !ok {
			continue
		}
		if backend, ok := parseFirstBackendRef(item, state.namespace); ok {
			state.backend = backend
			break
		}
	}
	if state.backend.name == "" {
		return nil
	}
	return state
}

func parseParentRefs(obj map[string]interface{}) []k8sParentRef {
	items, _, _ := unstructured.NestedSlice(obj, "spec", "parentRefs")
	out := make([]k8sParentRef, 0, len(items))
	for _, raw := range items {
		item, ok := raw.(map[string]interface{})
		if !ok {
			continue
		}
		name, _, _ := unstructured.NestedString(item, "name")
		if name == "" {
			continue
		}
		namespace, _, _ := unstructured.NestedString(item, "namespace")
		if namespace == "" {
			namespace, _, _ = unstructured.NestedString(obj, "metadata", "namespace")
		}
		sectionName, _, _ := unstructured.NestedString(item, "sectionName")
		out = append(out, k8sParentRef{
			gatewayKey:  namespace + "/" + name,
			sectionName: sectionName,
		})
	}
	return out
}

func parseFirstBackendRef(obj map[string]interface{}, defaultNamespace string) (k8sBackendRef, bool) {
	backendRefs, _, _ := unstructured.NestedSlice(obj, "backendRefs")
	for _, raw := range backendRefs {
		item, ok := raw.(map[string]interface{})
		if !ok {
			continue
		}
		name, _, _ := unstructured.NestedString(item, "name")
		if name == "" {
			continue
		}
		namespace, _, _ := unstructured.NestedString(item, "namespace")
		if namespace == "" {
			namespace = defaultNamespace
		}
		port64, _, _ := unstructured.NestedInt64(item, "port")
		port := uint32(port64)
		if port == 0 {
			port = 80
		}
		return k8sBackendRef{namespace: namespace, name: name, port: port}, true
	}
	return k8sBackendRef{}, false
}

func nestedStringSlice(obj map[string]interface{}, fields ...string) []string {
	values, _, _ := unstructured.NestedStringSlice(obj, fields...)
	return values
}
