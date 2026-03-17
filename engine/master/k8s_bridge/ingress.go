package k8s_bridge

import (
	"context"
	"fmt"
	"sort"
	"strings"
	"time"

	enginepkg "github.com/jabberwocky238/luna-edge/engine"
	"github.com/jabberwocky238/luna-edge/repository"
	"github.com/jabberwocky238/luna-edge/repository/metadata"
	networkingv1 "k8s.io/api/networking/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/fields"
	"k8s.io/client-go/informers"
	"k8s.io/client-go/kubernetes"
	"k8s.io/client-go/rest"
	"k8s.io/client-go/tools/cache"
)

type IngressBridge struct {
	namespace    string
	ingressClass string
	client       kubernetes.Interface
	factory      informers.SharedInformerFactory
	stopCh       chan struct{}
	repo         repository.Repository
	publisher    publisher

	ingresses map[string]*networkingv1.Ingress
}

func NewIngressBridge(namespace, ingressClass string, repo repository.Repository, pub publisher) (*IngressBridge, error) {
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
	return NewIngressBridgeWithClient(namespace, ingressClass, client, repo, pub), nil
}

func NewIngressBridgeWithClient(namespace, ingressClass string, client kubernetes.Interface, repo repository.Repository, pub publisher) *IngressBridge {
	if namespace == "" {
		namespace = enginepkg.POD_NAMESPACE
	}
	if namespace == "" {
		namespace = "default"
	}
	bridge := &IngressBridge{
		namespace:    namespace,
		ingressClass: strings.TrimSpace(ingressClass),
		client:       client,
		stopCh:       make(chan struct{}),
		repo:         repo,
		publisher:    pub,
		ingresses:    map[string]*networkingv1.Ingress{},
	}
	bridge.ensureInformer()
	return bridge
}

func (b *IngressBridge) LoadInitial(ctx context.Context) error {
	if b == nil || b.client == nil {
		return nil
	}
	list, err := b.client.NetworkingV1().Ingresses(b.namespace).List(ctx, metav1.ListOptions{})
	if err != nil {
		return fmt.Errorf("list ingresses: %w", err)
	}
	b.ingresses = map[string]*networkingv1.Ingress{}
	affected := map[string]struct{}{}
	for i := range list.Items {
		ing := list.Items[i].DeepCopy()
		if !b.matchesIngressClass(ing) {
			continue
		}
		b.ingresses[ing.Namespace+"/"+ing.Name] = ing
		for _, host := range ingressHosts(ing) {
			affected[host] = struct{}{}
		}
	}
	return b.syncHosts(ctx, mapKeys(affected), nil)
}

func (b *IngressBridge) Listen() {
	if b == nil || b.factory == nil {
		return
	}
	b.factory.Start(b.stopCh)
}

func (b *IngressBridge) Stop() error {
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

func (b *IngressBridge) ensureInformer() {
	if b == nil || b.client == nil || b.factory != nil {
		return
	}
	tweak := func(options *metav1.ListOptions) {
		options.FieldSelector = fields.Everything().String()
	}
	b.factory = informers.NewSharedInformerFactoryWithOptions(
		b.client,
		30*time.Second,
		informers.WithNamespace(b.namespace),
		informers.WithTweakListOptions(tweak),
	)
	b.factory.Networking().V1().Ingresses().Informer().AddEventHandler(cache.ResourceEventHandlerFuncs{
		AddFunc: func(obj interface{}) { b.storeIngress(obj) },
		UpdateFunc: func(_, newObj interface{}) { b.storeIngress(newObj) },
		DeleteFunc: func(obj interface{}) { b.deleteIngress(obj) },
	})
}

func (b *IngressBridge) storeIngress(obj interface{}) {
	ing, ok := obj.(*networkingv1.Ingress)
	if !ok || ing == nil {
		return
	}
	key := ing.Namespace + "/" + ing.Name
	oldHosts := map[string]struct{}{}
	if old := b.ingresses[key]; old != nil {
		for _, host := range ingressHosts(old) {
			oldHosts[host] = struct{}{}
		}
	}
	if !b.matchesIngressClass(ing) {
		delete(b.ingresses, key)
		_ = b.syncHosts(context.Background(), nil, mapKeys(oldHosts))
		return
	}
	b.ingresses[key] = ing.DeepCopy()
	newHosts := map[string]struct{}{}
	for _, host := range ingressHosts(ing) {
		newHosts[host] = struct{}{}
		oldHosts[host] = struct{}{}
	}
	_ = b.syncHosts(context.Background(), mapKeys(newHosts), diffKeys(oldHosts, newHosts))
}

func (b *IngressBridge) deleteIngress(obj interface{}) {
	deleteByNamespaceName(obj, func(namespace, name string) {
		key := namespace + "/" + name
		oldHosts := map[string]struct{}{}
		if old := b.ingresses[key]; old != nil {
			for _, host := range ingressHosts(old) {
				oldHosts[host] = struct{}{}
			}
		}
		delete(b.ingresses, key)
		_ = b.syncHosts(context.Background(), nil, mapKeys(oldHosts))
	})
}

func (b *IngressBridge) syncHosts(ctx context.Context, affectedHosts, removedHosts []string) error {
	next := b.materializeByHost(affectedHosts)
	return syncDomainSet(ctx, b.repo, b.publisher, next, affectedHosts, removedHosts)
}

func (b *IngressBridge) materializeByHost(hosts []string) map[string]domainMaterialized {
	hostSet := map[string]struct{}{}
	for _, host := range hosts {
		if normalized := normalizeHost(host); normalized != "" {
			hostSet[normalized] = struct{}{}
		}
	}
	out := map[string]domainMaterialized{}
	for _, ing := range b.ingresses {
		tlsHosts := ingressTLSHostSet(ing)
		for _, rule := range ing.Spec.Rules {
			host := normalizeHost(rule.Host)
			if host == "" {
				continue
			}
			if _, ok := hostSet[host]; !ok {
				continue
			}
			item := out[host]
			if item.domain.ID == "" {
				item.domain = metadata.DomainEndpoint{
					ID:          "k8s:domain:" + host,
					Hostname:    host,
					NeedCert:    tlsHosts[host],
					BackendType: metadata.BackendTypeL7HTTP,
				}
				if tlsHosts[host] {
					item.domain.BackendType = metadata.BackendTypeL7HTTPS
				}
			}
			if tlsHosts[host] {
				item.domain.NeedCert = true
				item.domain.BackendType = metadata.BackendTypeL7HTTPS
			}
			if rule.HTTP != nil {
				for idx, path := range rule.HTTP.Paths {
					if path.Backend.Service == nil {
						continue
					}
					backendID := fmt.Sprintf("k8s:backend:ingress:%s:%s:%s:%d", ing.Namespace, ing.Name, host, idx)
					item.backends = append(item.backends, metadata.ServiceBackendRef{
						ID:               backendID,
						ServiceNamespace: ing.Namespace,
						ServiceName:      path.Backend.Service.Name,
						ServicePort:      ingressServicePort(path.Backend.Service.Port),
					})
					priority := int32(len(normalizePath(path.Path)))
					if path.PathType != nil && *path.PathType == networkingv1.PathTypeExact {
						priority += 100000
					}
					item.routes = append(item.routes, metadata.HTTPRoute{
						ID:               fmt.Sprintf("k8s:route:ingress:%s:%s:%s:%d", ing.Namespace, ing.Name, host, idx),
						DomainEndpointID: item.domain.ID,
						Hostname:         host,
						Path:             normalizePath(path.Path),
						Priority:         priority,
						BackendRefID:     backendID,
					})
				}
			}
			out[host] = item
		}
	}
	for host := range out {
		item := out[host]
		dedupBackends(&item)
		out[host] = item
	}
	return out
}

func (b *IngressBridge) matchesIngressClass(ing *networkingv1.Ingress) bool {
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

func ingressHosts(ing *networkingv1.Ingress) []string {
	if ing == nil {
		return nil
	}
	seen := map[string]struct{}{}
	var out []string
	for _, rule := range ing.Spec.Rules {
		host := normalizeHost(rule.Host)
		if host == "" {
			continue
		}
		if _, ok := seen[host]; ok {
			continue
		}
		seen[host] = struct{}{}
		out = append(out, host)
	}
	for _, tls := range ing.Spec.TLS {
		for _, host := range tls.Hosts {
			normalized := normalizeHost(host)
			if normalized == "" {
				continue
			}
			if _, ok := seen[normalized]; ok {
				continue
			}
			seen[normalized] = struct{}{}
			out = append(out, normalized)
		}
	}
	sort.Strings(out)
	return out
}

func ingressTLSHostSet(ing *networkingv1.Ingress) map[string]bool {
	out := map[string]bool{}
	if ing == nil {
		return out
	}
	for _, tls := range ing.Spec.TLS {
		for _, host := range tls.Hosts {
			if normalized := normalizeHost(host); normalized != "" {
				out[normalized] = true
			}
		}
	}
	return out
}

func ingressServicePort(port networkingv1.ServiceBackendPort) uint32 {
	if port.Number > 0 {
		return uint32(port.Number)
	}
	return 80
}

func dedupBackends(item *domainMaterialized) {
	if item == nil {
		return
	}
	seen := map[string]metadata.ServiceBackendRef{}
	for i := range item.backends {
		seen[item.backends[i].ID] = item.backends[i]
	}
	item.backends = item.backends[:0]
	for _, backend := range seen {
		item.backends = append(item.backends, backend)
	}
	sort.Slice(item.backends, func(i, j int) bool { return item.backends[i].ID < item.backends[j].ID })
}

func mapKeys(values map[string]struct{}) []string {
	out := make([]string, 0, len(values))
	for key := range values {
		out = append(out, key)
	}
	sort.Strings(out)
	return out
}

func diffKeys(oldSet, newSet map[string]struct{}) []string {
	var out []string
	for key := range oldSet {
		if _, ok := newSet[key]; ok {
			continue
		}
		out = append(out, key)
	}
	sort.Strings(out)
	return out
}
