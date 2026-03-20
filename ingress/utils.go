package ingress

import (
	"fmt"
	"os"
	"regexp"
	"strings"

	"github.com/jabberwocky238/luna-edge/repository/metadata"
	"k8s.io/apimachinery/pkg/api/meta"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/runtime"
	"k8s.io/client-go/tools/cache"
)

var hostnameLabelPattern = regexp.MustCompile(`^[a-z0-9](?:[a-z0-9-]{0,61}[a-z0-9])?$`)

func normalizeHost(host string) string {
	host = strings.TrimSpace(strings.ToLower(host))
	host = strings.TrimPrefix(host, "http://")
	host = strings.TrimPrefix(host, "https://")
	if idx := strings.Index(host, ":"); idx >= 0 {
		host = host[:idx]
	}
	return sanitizeHostname(host)
}

func sanitizeHostname(host string) string {
	host = strings.TrimSpace(strings.ToLower(host))
	if host == "" {
		return ""
	}

	wildcard := false
	if strings.HasPrefix(host, "*.") {
		wildcard = true
		host = strings.TrimPrefix(host, "*.")
	}
	if host == "" || strings.Contains(host, "..") || strings.ContainsAny(host, `/\ `) {
		return ""
	}

	labels := strings.Split(host, ".")
	for _, label := range labels {
		if !hostnameLabelPattern.MatchString(label) {
			return ""
		}
	}

	if wildcard {
		return "*." + host
	}
	return host
}

func buildUpstreamURL(protocol, address string, port uint32) string {
	protocol = strings.TrimSpace(strings.ToLower(protocol))
	if protocol == "" {
		protocol = "http"
	}
	address = strings.TrimSpace(address)
	if address == "" {
		return ""
	}
	if strings.HasPrefix(address, "http://") || strings.HasPrefix(address, "https://") {
		return address
	}
	if strings.Contains(address, ":") {
		return fmt.Sprintf("%s://%s", protocol, address)
	}
	if port > 0 {
		return fmt.Sprintf("%s://%s:%d", protocol, address, port)
	}
	return fmt.Sprintf("%s://%s", protocol, address)
}

func fileExists(path string) bool {
	info, err := os.Stat(path)
	if err != nil {
		return false
	}
	return !info.IsDir()
}

func resolveCertLRUSize(size int) int {
	if size <= 0 {
		return DefaultIngressLRUSize
	}
	return size
}

func initBridgeHandlers(bridge *K8sBridge) {
	bridge.ensureIngressInformer()
	if bridge.ingressFactory != nil {
		bridge.ingressFactory.Networking().V1().Ingresses().Informer().AddEventHandler(cache.ResourceEventHandlerFuncs{
			AddFunc: func(obj interface{}) {
				bridge.storeIngress(obj)
			},
			UpdateFunc: func(_, newObj interface{}) {
				bridge.storeIngress(newObj)
			},
			DeleteFunc: func(obj interface{}) {
				bridge.deleteIngress(obj)
			},
		})
		bridge.ingressFactory.Core().V1().Services().Informer().AddEventHandler(cache.ResourceEventHandlerFuncs{
			AddFunc: func(obj interface{}) {
				bridge.storeService(obj)
			},
			UpdateFunc: func(_, newObj interface{}) {
				bridge.storeService(newObj)
			},
			DeleteFunc: func(obj interface{}) {
				bridge.deleteService(obj)
			},
		})
	}

	bridge.ensureGatewayInformers()
}

func deleteByNamespaceName(obj interface{}, deleter func(namespace, name string)) {
	switch value := obj.(type) {
	case metav1.Object:
		deleter(value.GetNamespace(), value.GetName())
	case cache.DeletedFinalStateUnknown:
		accessor, ok := value.Obj.(metav1.Object)
		if ok && accessor != nil {
			deleter(accessor.GetNamespace(), accessor.GetName())
			return
		}
		ro, ok := value.Obj.(runtime.Object)
		if !ok || ro == nil {
			return
		}
		accessor, err := meta.Accessor(ro)
		if err == nil {
			deleter(accessor.GetNamespace(), accessor.GetName())
		}
	}
}

func buildServiceAddress(name, namespace string) string {
	return fmt.Sprintf("%s.%s.svc.cluster.local", name, namespace)
}

func backendAddress(ref *metadata.ServiceBackendRef) string {
	if ref == nil {
		return ""
	}
	if ref.Type == metadata.ServiceBackendTypeExternal {
		return strings.TrimSpace(ref.ArbitraryEndpoint)
	}
	return buildServiceAddress(ref.ServiceName, ref.ServiceNamespace)
}

func metav1ObjectMeta(namespace, name string, generation int64) metav1.ObjectMeta {
	return metav1.ObjectMeta{Namespace: namespace, Name: name, Generation: generation}
}
