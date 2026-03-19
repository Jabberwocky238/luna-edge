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
	corev1 "k8s.io/api/core/v1"
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
	OnUpdate     func(ctx context.Context, fqdn string) error
	stopCh       chan struct{}
	ctx          context.Context
	repo         repository.Repository

	ingresses map[string]*networkingv1.Ingress
	services  map[string]*corev1.Service
}

func NewIngressBridge(namespace, ingressClass string, repo repository.Repository, onDomainChange func(ctx context.Context, fqdn string) error) (*IngressBridge, error) {
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
	return NewIngressBridgeWithClient(namespace, ingressClass, client, repo, onDomainChange), nil
}

func NewIngressBridgeWithClient(namespace, ingressClass string, client kubernetes.Interface, repo repository.Repository, onDomainChange func(ctx context.Context, fqdn string) error) *IngressBridge {
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
		ingresses:    map[string]*networkingv1.Ingress{},
		services:     map[string]*corev1.Service{},
		OnUpdate:     onDomainChange,
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
	services, err := b.client.CoreV1().Services(b.namespace).List(ctx, metav1.ListOptions{})
	if err != nil {
		return fmt.Errorf("list services: %w", err)
	}
	b.ingresses = map[string]*networkingv1.Ingress{}
	b.services = map[string]*corev1.Service{}
	affected := map[string]struct{}{}
	for i := range services.Items {
		svc := services.Items[i].DeepCopy()
		b.services[svc.Namespace+"/"+svc.Name] = svc
	}
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

func (b *IngressBridge) Listen(ctx context.Context) {
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
		AddFunc:    func(obj interface{}) { b.storeIngress(obj) },
		UpdateFunc: func(_, newObj interface{}) { b.storeIngress(newObj) },
		DeleteFunc: func(obj interface{}) { b.deleteIngress(obj) },
	})
	b.factory.Core().V1().Services().Informer().AddEventHandler(cache.ResourceEventHandlerFuncs{
		AddFunc:    func(obj interface{}) { b.storeService(obj) },
		UpdateFunc: func(_, newObj interface{}) { b.storeService(newObj) },
		DeleteFunc: func(obj interface{}) { b.deleteService(obj) },
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
		_ = b.syncHosts(b.runtimeContext(), nil, mapKeys(oldHosts))
		return
	}
	b.ingresses[key] = ing.DeepCopy()
	newHosts := map[string]struct{}{}
	for _, host := range ingressHosts(ing) {
		newHosts[host] = struct{}{}
		oldHosts[host] = struct{}{}
	}
	_ = b.syncHosts(b.runtimeContext(), mapKeys(newHosts), diffKeys(oldHosts, newHosts))
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
		_ = b.syncHosts(b.runtimeContext(), nil, mapKeys(oldHosts))
	})
}

func (b *IngressBridge) storeService(obj interface{}) {
	svc, ok := obj.(*corev1.Service)
	if !ok || svc == nil {
		return
	}
	b.services[svc.Namespace+"/"+svc.Name] = svc.DeepCopy()
	affected := map[string]struct{}{}
	for _, ing := range b.ingresses {
		if ing.Namespace != svc.Namespace {
			continue
		}
		if ingressReferencesService(ing, svc.Name) {
			for _, host := range ingressHosts(ing) {
				affected[host] = struct{}{}
			}
		}
	}
	if len(affected) > 0 {
		_ = b.syncHosts(b.runtimeContext(), mapKeys(affected), nil)
	}
}

func (b *IngressBridge) deleteService(obj interface{}) {
	deleteByNamespaceName(obj, func(namespace, name string) {
		delete(b.services, namespace+"/"+name)
		affected := map[string]struct{}{}
		for _, ing := range b.ingresses {
			if ing.Namespace != namespace {
				continue
			}
			if ingressReferencesService(ing, name) {
				for _, host := range ingressHosts(ing) {
					affected[host] = struct{}{}
				}
			}
		}
		if len(affected) > 0 {
			_ = b.syncHosts(b.runtimeContext(), mapKeys(affected), nil)
		}
	})
}

func (b *IngressBridge) runtimeContext() context.Context {
	if b != nil && b.ctx != nil {
		return b.ctx
	}
	return context.Background()
}

func (b *IngressBridge) syncHosts(ctx context.Context, affectedHosts, removedHosts []string) error {
	next := b.materializeByHost(affectedHosts)
	changedAffected, changedRemoved, err := syncDomainSet(ctx, b.repo, next, affectedHosts, removedHosts)
	if err != nil {
		return err
	}
	for _, host := range changedAffected {
		if err := b.OnUpdate(ctx, host); err != nil {
			return err
		}
	}
	for _, host := range changedRemoved {
		if err := b.OnUpdate(ctx, host); err != nil {
			return err
		}
	}
	return nil
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
			item.domain = metadata.DomainEndpoint{
				Hostname:    host,
				NeedCert:    tlsHosts[host],
				BackendType: metadata.BackendTypeL7HTTP,
			}
			if tlsHosts[host] {
				item.domain.BackendType = metadata.BackendTypeL7HTTPS
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
					backend := metadata.ServiceBackendRef{
						ID:               backendID,
						Type:             metadata.ServiceBackendTypeSVC,
						ServiceNamespace: ing.Namespace,
						ServiceName:      path.Backend.Service.Name,
						Port:             ingressServicePort(path.Backend.Service.Port),
					}
					if resolved, ok := b.resolveIngressBackendServiceAddress(ing.Namespace, path.Backend.Service.Name); ok {
						backend.Type = metadata.ServiceBackendTypeExternal
						backend.ArbitraryEndpoint = resolved
						backend.ServiceNamespace = ""
						backend.ServiceName = ""
					}
					item.backends = append(item.backends, backend)
					priority := int32(len(normalizePath(path.Path)))
					if path.PathType != nil && *path.PathType == networkingv1.PathTypeExact {
						priority += 100000
					}
					item.routes = append(item.routes, metadata.HTTPRoute{
						ID:           fmt.Sprintf("k8s:route:ingress:%s:%s:%s:%d", ing.Namespace, ing.Name, host, idx),
						Hostname:     item.domain.Hostname,
						Path:         normalizePath(path.Path),
						Priority:     priority,
						BackendRefID: backendID,
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

func (b *IngressBridge) resolveIngressBackendServiceAddress(namespace, name string) (string, bool) {
	svc := b.services[namespace+"/"+name]
	if svc == nil {
		return "", false
	}
	if svc.Spec.Type != corev1.ServiceTypeExternalName {
		return "", false
	}
	address := normalizeArbitraryEndpoint(svc.Spec.ExternalName)
	if address == "" {
		return "", false
	}
	return address, true
}

func ingressReferencesService(ing *networkingv1.Ingress, serviceName string) bool {
	if ing == nil || serviceName == "" {
		return false
	}
	for _, rule := range ing.Spec.Rules {
		if rule.HTTP == nil {
			continue
		}
		for _, path := range rule.HTTP.Paths {
			if path.Backend.Service != nil && path.Backend.Service.Name == serviceName {
				return true
			}
		}
	}
	return false
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
