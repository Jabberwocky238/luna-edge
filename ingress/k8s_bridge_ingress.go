package ingress

import (
	"context"
	"encoding/json"
	"fmt"
	"time"

	corev1 "k8s.io/api/core/v1"
	networkingv1 "k8s.io/api/networking/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/fields"
	"k8s.io/client-go/informers"
)

type k8sIngressState struct {
	resource *networkingv1.Ingress
}

func (b *K8sBridge) ensureIngressInformer() {
	if b.client == nil || b.ingressFactory != nil {
		return
	}
	tweak := func(options *metav1.ListOptions) {
		options.FieldSelector = fields.Everything().String()
	}
	b.ingressFactory = informers.NewSharedInformerFactoryWithOptions(
		b.client,
		30*time.Second,
		informers.WithNamespace(b.namespace),
		informers.WithTweakListOptions(tweak),
	)
}

func (b *K8sBridge) loadInitialIngresses(ctx context.Context) error {
	if b.client == nil {
		return nil
	}
	list, err := b.client.NetworkingV1().Ingresses(b.namespace).List(ctx, metav1.ListOptions{})
	if err != nil {
		return fmt.Errorf("list ingresses: %w", err)
	}

	b.mu.Lock()
	defer b.mu.Unlock()
	for i := range list.Items {
		ing := list.Items[i]
		if !b.matchesIngressClass(&ing) {
			continue
		}
		b.ingresses[ing.Namespace+"/"+ing.Name] = &k8sIngressState{resource: ing.DeepCopy()}
	}
	return nil
}

func (b *K8sBridge) loadInitialServices(ctx context.Context) error {
	if b.client == nil {
		return nil
	}
	list, err := b.client.CoreV1().Services(b.namespace).List(ctx, metav1.ListOptions{})
	if err != nil {
		return fmt.Errorf("list services: %w", err)
	}

	b.mu.Lock()
	defer b.mu.Unlock()
	for i := range list.Items {
		svc := list.Items[i]
		b.services[svc.Namespace+"/"+svc.Name] = &k8sServiceState{resource: svc.DeepCopy()}
	}
	return nil
}

// ListIngresses 返回当前缓存中的标准 Ingress 快照。
func (b *K8sBridge) ListIngresses() []*networkingv1.Ingress {
	b.mu.RLock()
	defer b.mu.RUnlock()

	out := make([]*networkingv1.Ingress, 0, len(b.ingresses))
	for _, ing := range b.ingresses {
		out = append(out, ing.resource.DeepCopy())
	}
	return out
}

func (b *K8sBridge) storeIngress(obj interface{}) {
	ing, ok := obj.(*networkingv1.Ingress)
	if !ok || ing == nil {
		return
	}
	b.mu.Lock()
	if !b.matchesIngressClass(ing) {
		delete(b.ingresses, ing.Namespace+"/"+ing.Name)
		b.rebuildRoutesLocked()
		b.mu.Unlock()
		return
	}
	b.ingresses[ing.Namespace+"/"+ing.Name] = &k8sIngressState{resource: ing.DeepCopy()}
	b.rebuildRoutesLocked()
	b.mu.Unlock()
}

func (b *K8sBridge) storeService(obj interface{}) {
	svc, ok := obj.(*corev1.Service)
	if !ok || svc == nil {
		return
	}
	b.mu.Lock()
	b.services[svc.Namespace+"/"+svc.Name] = &k8sServiceState{resource: svc.DeepCopy()}
	b.rebuildRoutesLocked()
	b.mu.Unlock()
}

func (b *K8sBridge) deleteIngress(obj interface{}) {
	deleteByNamespaceName(obj, func(namespace, name string) {
		b.mu.Lock()
		delete(b.ingresses, namespace+"/"+name)
		b.rebuildRoutesLocked()
		b.mu.Unlock()
	})
}

func (b *K8sBridge) deleteService(obj interface{}) {
	deleteByNamespaceName(obj, func(namespace, name string) {
		b.mu.Lock()
		delete(b.services, namespace+"/"+name)
		b.rebuildRoutesLocked()
		b.mu.Unlock()
	})
}

func (b *K8sBridge) matchesIngressClass(ing *networkingv1.Ingress) bool {
	if ing == nil {
		return false
	}
	expected := b.ingressClass
	if expected == "" {
		return false
	}
	if ing.Spec.IngressClassName != nil && *ing.Spec.IngressClassName != "" {
		return *ing.Spec.IngressClassName == expected
	}
	if annotations := ing.GetAnnotations(); annotations != nil {
		return annotations["kubernetes.io/ingress.class"] == expected
	}
	return false
}

func (b *K8sBridge) rebuildIngressRoutesLocked() {
	for _, item := range b.ingresses {
		ing := item.resource
		for _, rule := range ing.Spec.Rules {
			host := normalizeHost(rule.Host)
			if host == "" || rule.HTTP == nil {
				continue
			}
			for idx, path := range rule.HTTP.Paths {
				service := path.Backend.Service
				if service == nil {
					continue
				}

				servicePort := uint32(80)
				if service.Port.Number > 0 {
					servicePort = uint32(service.Port.Number)
				}
				if service.Port.Name != "" && servicePort == 0 {
					servicePort = 80
				}
				serviceAddress := buildServiceAddress(service.Name, ing.Namespace)
				if resolved, ok := b.resolveIngressBackendServiceAddressLocked(ing.Namespace, service.Name); ok {
					serviceAddress = resolved
				}

				bindingID := fmt.Sprintf("k8s:ingress:%s:%s:%s:%d", ing.Namespace, ing.Name, host, idx)
				routeVersion := uint64(ing.Generation)
				if routeVersion == 0 {
					routeVersion = 1
				}

				pathKind := k8sRoutePathPrefix
				if path.PathType != nil && *path.PathType == networkingv1.PathTypeExact {
					pathKind = k8sRoutePathExact
				}
				normalizedPath := normalizeIngressPath(path.Path)
				routeJSON, _ := json.Marshal(path)

				b.httpResolved[host] = append(b.httpResolved[host], k8sMaterializedRoute{
					kind:     RouteKindHTTP,
					hostname: host,
					port:     80,
					binding: &BackendBinding{
						ID:           bindingID,
						Hostname:     host,
						ServiceID:    fmt.Sprintf("%s/%s", ing.Namespace, service.Name),
						Namespace:    ing.Namespace,
						Name:         service.Name,
						Address:      serviceAddress,
						Port:         servicePort,
						Protocol:     RouteKindHTTP,
						RouteVersion: routeVersion,
						Path:         normalizedPath,
						BackendJSON:  string(routeJSON),
					},
					route: &ResolvedRoute{
						DomainID:     bindingID,
						Hostname:     host,
						RouteVersion: routeVersion,
						Protocol:     RouteKindHTTP,
						RouteJSON:    string(routeJSON),
						BindingID:    bindingID,
					},
					path:     normalizedPath,
					pathKind: pathKind,
				})
			}
		}
	}
}

func (b *K8sBridge) resolveIngressBackendServiceAddressLocked(namespace, name string) (string, bool) {
	state := b.services[namespace+"/"+name]
	if state == nil || state.resource == nil {
		return "", false
	}
	if state.resource.Spec.Type != corev1.ServiceTypeExternalName {
		return "", false
	}
	address := normalizeHost(state.resource.Spec.ExternalName)
	if address == "" {
		return "", false
	}
	return address, true
}
