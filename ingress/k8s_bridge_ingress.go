package ingress

import (
	"context"
	"encoding/json"
	"fmt"
	"time"

	"github.com/jabberwocky238/luna-edge/repository/metadata"
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
	defer b.mu.Unlock()
	if !b.matchesIngressClass(ing) {
		delete(b.ingresses, ing.Namespace+"/"+ing.Name)
		b.rebuildRoutesLocked()
		return
	}
	b.ingresses[ing.Namespace+"/"+ing.Name] = &k8sIngressState{resource: ing.DeepCopy()}
	b.rebuildRoutesLocked()
}

func (b *K8sBridge) deleteIngress(obj interface{}) {
	deleteByNamespaceName(obj, func(namespace, name string) {
		b.mu.Lock()
		delete(b.ingresses, namespace+"/"+name)
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
					kind:     metadata.ServiceBindingRouteKindHTTP,
					hostname: host,
					port:     80,
					binding: &metadata.ServiceBinding{
						ID:           bindingID,
						DomainID:     bindingID,
						Hostname:     host,
						ServiceID:    fmt.Sprintf("%s/%s", ing.Namespace, service.Name),
						Namespace:    ing.Namespace,
						Name:         service.Name,
						Address:      buildServiceAddress(service.Name, ing.Namespace),
						Port:         servicePort,
						Protocol:     "http",
						RouteVersion: routeVersion,
						BackendJSON:  string(routeJSON),
					},
					route: &ResolvedRoute{
						DomainID:     bindingID,
						Hostname:     host,
						RouteVersion: routeVersion,
						Protocol:     metadata.ServiceBindingRouteKindHTTP,
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
