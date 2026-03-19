package k8s_bridge

import (
	"context"
	"strings"

	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/apis/meta/v1/unstructured"
)

type httpRouteState struct {
	key       string
	name      string
	namespace string
	hostnames []string
	parents   []parentRef
	rules     []httpRouteRule
}

type httpRouteRule struct {
	path    string
	exact   bool
	backend backendRef
}

func (b *GatewayBridge) loadHTTPRoutes(ctx context.Context) error {
	list, err := b.dynamicClient.Resource(httpRouteGVR).Namespace(b.namespace).List(ctx, metav1.ListOptions{})
	if err != nil {
		return err
	}
	b.mu.Lock()
	defer b.mu.Unlock()
	b.httpRoutes = map[string]*httpRouteState{}
	for i := range list.Items {
		if state := parseHTTPRouteState(list.Items[i].DeepCopy()); state != nil {
			b.httpRoutes[state.key] = state
		}
	}
	return nil
}

func (b *GatewayBridge) storeHTTPRoute(obj *unstructured.Unstructured) {
	if obj == nil {
		return
	}
	b.mu.Lock()
	oldHosts := b.collectHostsLocked()
	var affected []string
	if state := parseHTTPRouteState(obj); state != nil {
		b.httpRoutes[state.key] = state
		affected = normalizeHosts(state.hostnames)
	}
	newHosts := b.collectHostsLocked()
	_ = b.syncHostsLocked(b.runtimeContext(), affected, diffStrings(oldHosts, newHosts))
	b.mu.Unlock()
}

func (b *GatewayBridge) deleteHTTPRoute(obj interface{}) {
	b.mu.Lock()
	oldHosts := routeHostsFromDeletedObject(obj)
	deleteByNamespaceName(obj, func(namespace, name string) { delete(b.httpRoutes, namespace+"/"+name) })
	_ = b.syncHostsLocked(b.runtimeContext(), oldHosts, nil)
	b.mu.Unlock()
}

func parseHTTPRouteState(obj *unstructured.Unstructured) *httpRouteState {
	if obj == nil {
		return nil
	}
	state := &httpRouteState{
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
		path := "/"
		exact := false
		matches, _, _ := unstructured.NestedSlice(item, "matches")
		if len(matches) > 0 {
			if match, ok := matches[0].(map[string]interface{}); ok {
				if value, found, _ := unstructured.NestedString(match, "path", "value"); found && value != "" {
					path = value
				}
				if typ, found, _ := unstructured.NestedString(match, "path", "type"); found && strings.EqualFold(typ, "Exact") {
					exact = true
				}
			}
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
				port = 80
			}
			state.rules = append(state.rules, httpRouteRule{
				path:  path,
				exact: exact,
				backend: backendRef{
					namespace: namespace,
					name:      name,
					port:      port,
				},
			})
			break
		}
	}
	return state
}
