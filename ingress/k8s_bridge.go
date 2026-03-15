package ingress

import (
	"context"
	"encoding/json"
	"fmt"
	"os"
	"strings"
	"sync"
	"time"

	"github.com/jabberwocky238/luna-edge/repository/metadata"
	networkingv1 "k8s.io/api/networking/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/fields"
	"k8s.io/client-go/informers"
	"k8s.io/client-go/kubernetes"
	"k8s.io/client-go/rest"
	"k8s.io/client-go/tools/cache"
)

// K8sBridge 负责加载并监听当前命名空间内的标准 Kubernetes Ingress。
type K8sBridge struct {
	namespace    string
	ingressClass string
	client       kubernetes.Interface
	factory      informers.SharedInformerFactory
	informer     cache.SharedIndexInformer
	stopCh       chan struct{}

	mu        sync.RWMutex
	ingresses map[string]*networkingv1.Ingress
	routes    map[string][]k8sMaterializedRoute
}

type k8sMaterializedRoute struct {
	binding  *metadata.ServiceBinding
	route    *metadata.RouteProjection
	path     string
	pathType networkingv1.PathType
}

// NewK8sBridge 创建一个只处理当前命名空间标准 Ingress 的 bridge。
func NewK8sBridge(namespace, ingressClass string) (*K8sBridge, error) {
	if namespace == "" {
		namespace = os.Getenv("POD_NAMESPACE")
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
	return NewK8sBridgeWithClient(namespace, ingressClass, client), nil
}

// NewK8sBridgeWithClient 创建使用显式 client 的 bridge，便于 fake kube 测试。
func NewK8sBridgeWithClient(namespace, ingressClass string, client kubernetes.Interface) *K8sBridge {
	if namespace == "" {
		namespace = os.Getenv("POD_NAMESPACE")
	}
	if namespace == "" {
		namespace = "default"
	}
	tweak := func(options *metav1.ListOptions) {
		options.FieldSelector = fields.Everything().String()
	}
	factory := informers.NewSharedInformerFactoryWithOptions(
		client,
		30*time.Second,
		informers.WithNamespace(namespace),
		informers.WithTweakListOptions(tweak),
	)
	informer := factory.Networking().V1().Ingresses().Informer()

	bridge := &K8sBridge{
		namespace:    namespace,
		ingressClass: normalizeHost(strings.TrimSpace(ingressClass)),
		client:       client,
		factory:      factory,
		informer:     informer,
		stopCh:       make(chan struct{}),
		ingresses:    make(map[string]*networkingv1.Ingress),
		routes:       make(map[string][]k8sMaterializedRoute),
	}
	initBridgeHandlers(bridge)
	return bridge
}

// LoadInitial 全量加载当前命名空间已有的 Ingress。
func (b *K8sBridge) LoadInitial(ctx context.Context) error {
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
		b.ingresses[ing.Namespace+"/"+ing.Name] = ing.DeepCopy()
	}
	b.rebuildRoutesLocked()
	return nil
}

// Listen 启动 informer 监听当前命名空间 Ingress 变化。
func (b *K8sBridge) Listen() {
	b.factory.Start(b.stopCh)
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

// Namespace 返回 bridge 当前监听的命名空间。
func (b *K8sBridge) Namespace() string {
	return b.namespace
}

// ListIngresses 返回当前缓存中的标准 Ingress 快照。
func (b *K8sBridge) ListIngresses() []*networkingv1.Ingress {
	b.mu.RLock()
	defer b.mu.RUnlock()

	out := make([]*networkingv1.Ingress, 0, len(b.ingresses))
	for _, ing := range b.ingresses {
		out = append(out, ing.DeepCopy())
	}
	return out
}

// ResolveHost 根据 host 返回默认路径命中的绑定与路由。
func (b *K8sBridge) ResolveHost(host string) (*metadata.ServiceBinding, *metadata.RouteProjection, bool) {
	return b.ResolveRequest(host, "/")
}

// ResolveRequest 根据 host + path 返回由标准 Ingress 翻译出的绑定与路由。
func (b *K8sBridge) ResolveRequest(host, requestPath string) (*metadata.ServiceBinding, *metadata.RouteProjection, bool) {
	host = normalizeHost(host)
	if host == "" {
		return nil, nil, false
	}
	requestPath = normalizeIngressPath(requestPath)

	b.mu.RLock()
	defer b.mu.RUnlock()

	routes := b.routes[host]
	selected, ok := selectK8sRoute(routes, requestPath)
	if !ok {
		return nil, nil, false
	}
	return cloneBinding(selected.binding), cloneRoute(selected.route), true
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
	b.ingresses[ing.Namespace+"/"+ing.Name] = ing.DeepCopy()
	b.rebuildRoutesLocked()
}

func (b *K8sBridge) deleteIngress(obj interface{}) {
	switch value := obj.(type) {
	case *networkingv1.Ingress:
		b.mu.Lock()
		delete(b.ingresses, value.Namespace+"/"+value.Name)
		b.rebuildRoutesLocked()
		b.mu.Unlock()
	case cache.DeletedFinalStateUnknown:
		ing, ok := value.Obj.(*networkingv1.Ingress)
		if !ok || ing == nil {
			return
		}
		b.mu.Lock()
		delete(b.ingresses, ing.Namespace+"/"+ing.Name)
		b.rebuildRoutesLocked()
		b.mu.Unlock()
	}
}

func (b *K8sBridge) matchesIngressClass(ing *networkingv1.Ingress) bool {
	if ing == nil {
		return false
	}
	expected := strings.TrimSpace(b.ingressClass)
	if expected == "" {
		return false
	}
	if ing.Spec.IngressClassName != nil && strings.TrimSpace(*ing.Spec.IngressClassName) != "" {
		return strings.TrimSpace(*ing.Spec.IngressClassName) == expected
	}
	if annotations := ing.GetAnnotations(); annotations != nil {
		return strings.TrimSpace(annotations["kubernetes.io/ingress.class"]) == expected
	}
	return false
}

func (b *K8sBridge) rebuildRoutesLocked() {
	b.routes = make(map[string][]k8sMaterializedRoute)
	for _, ing := range b.ingresses {
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

				bindingID := fmt.Sprintf("k8s:%s:%s:%s:%d", ing.Namespace, ing.Name, host, idx)
				routeVersion := uint64(ing.Generation)
				if routeVersion == 0 {
					routeVersion = 1
				}

				pathType := networkingv1.PathTypeImplementationSpecific
				if path.PathType != nil {
					pathType = *path.PathType
				}
				normalizedPath := normalizeIngressPath(path.Path)
				routeJSON, _ := json.Marshal(path)

				b.routes[host] = append(b.routes[host], k8sMaterializedRoute{
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
					route: &metadata.RouteProjection{
						DomainID:     bindingID,
						Hostname:     host,
						RouteVersion: routeVersion,
						Protocol:     "http",
						RouteJSON:    string(routeJSON),
						BindingID:    bindingID,
					},
					path:     normalizedPath,
					pathType: pathType,
				})
			}
		}
	}
}

func normalizeIngressPath(path string) string {
	path = strings.TrimSpace(path)
	if path == "" || path[0] != '/' {
		return "/"
	}
	return path
}

func selectK8sRoute(routes []k8sMaterializedRoute, requestPath string) (k8sMaterializedRoute, bool) {
	var (
		selected k8sMaterializedRoute
		found    bool
	)
	for _, candidate := range routes {
		if !k8sPathMatches(candidate.pathType, candidate.path, requestPath) {
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
	if left.pathType != right.pathType {
		if left.pathType == networkingv1.PathTypeExact {
			return 1
		}
		if right.pathType == networkingv1.PathTypeExact {
			return -1
		}
	}
	if len(left.path) != len(right.path) {
		if len(left.path) > len(right.path) {
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

func k8sPathMatches(pathType networkingv1.PathType, routePath, requestPath string) bool {
	routePath = normalizeIngressPath(routePath)
	requestPath = normalizeIngressPath(requestPath)

	switch pathType {
	case networkingv1.PathTypeExact:
		return requestPath == routePath
	case networkingv1.PathTypePrefix, networkingv1.PathTypeImplementationSpecific:
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
