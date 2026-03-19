package k8s_bridge

import (
	"context"

	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/apis/meta/v1/unstructured"
)

type tlsRouteState struct {
	key       string
	name      string
	namespace string
	hostnames []string
	parents   []parentRef
	backend   backendRef
}

func (b *GatewayBridge) loadTLSRoutes(ctx context.Context) error {
	list, err := b.dynamicClient.Resource(tlsRouteGVR).Namespace(b.namespace).List(ctx, metav1.ListOptions{})
	if err != nil {
		return nil
	}
	b.mu.Lock()
	defer b.mu.Unlock()
	b.tlsRoutes = map[string]*tlsRouteState{}
	for i := range list.Items {
		if state := parseTLSRouteState(list.Items[i].DeepCopy()); state != nil {
			b.tlsRoutes[state.key] = state
		}
	}
	return nil
}

func (b *GatewayBridge) storeTLSRoute(obj *unstructured.Unstructured) {
	if obj == nil {
		return
	}
	b.mu.Lock()
	oldHosts := b.collectHostsLocked()
	var affected []string
	if state := parseTLSRouteState(obj); state != nil {
		b.tlsRoutes[state.key] = state
		affected = normalizeHosts(state.hostnames)
	}
	newHosts := b.collectHostsLocked()
	_ = b.syncHostsLocked(b.runtimeContext(), affected, diffStrings(oldHosts, newHosts))
	b.mu.Unlock()
}

func (b *GatewayBridge) deleteTLSRoute(obj interface{}) {
	b.mu.Lock()
	oldHosts := routeHostsFromDeletedObject(obj)
	deleteByNamespaceName(obj, func(namespace, name string) { delete(b.tlsRoutes, namespace+"/"+name) })
	_ = b.syncHostsLocked(b.runtimeContext(), oldHosts, nil)
	b.mu.Unlock()
}

func parseTLSRouteState(obj *unstructured.Unstructured) *tlsRouteState {
	if obj == nil {
		return nil
	}
	state := &tlsRouteState{
		key:       obj.GetNamespace() + "/" + obj.GetName(),
		name:      obj.GetName(),
		namespace: obj.GetNamespace(),
		hostnames: nestedStringSlice(obj.Object, "spec", "hostnames"),
	}
	parentRefs, _, _ := unstructured.NestedSlice(obj.Object, "spec", "parentRefs")
	for _, raw := range parentRefs {
		item, ok := raw.(map[string]interface{})
		if !ok {
			continue
		}
		name, _, _ := unstructured.NestedString(item, "name")
		namespace, _, _ := unstructured.NestedString(item, "namespace")
		if namespace == "" {
			namespace = obj.GetNamespace()
		}
		sectionName, _, _ := unstructured.NestedString(item, "sectionName")
		state.parents = append(state.parents, parentRef{gatewayKey: namespace + "/" + name, sectionName: sectionName})
	}
	rules, _, _ := unstructured.NestedSlice(obj.Object, "spec", "rules")
	for _, raw := range rules {
		item, ok := raw.(map[string]interface{})
		if !ok {
			continue
		}
		backendRefs, _, _ := unstructured.NestedSlice(item, "backendRefs")
		for _, backendRaw := range backendRefs {
			backendItem, ok := backendRaw.(map[string]interface{})
			if !ok {
				continue
			}
			name, _, _ := unstructured.NestedString(backendItem, "name")
			if name == "" {
				continue
			}
			namespace, _, _ := unstructured.NestedString(backendItem, "namespace")
			if namespace == "" {
				namespace = obj.GetNamespace()
			}
			port64, _, _ := unstructured.NestedInt64(backendItem, "port")
			port := uint32(port64)
			if port == 0 {
				port = 443
			}
			state.backend = backendRef{namespace: namespace, name: name, port: port}
			return state
		}
	}
	return nil
}
