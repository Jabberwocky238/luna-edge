package ingress

import (
	"testing"

	"github.com/jabberwocky238/luna-edge/repository/metadata"
	"k8s.io/apimachinery/pkg/apis/meta/v1/unstructured"
	"k8s.io/client-go/kubernetes/fake"
)

func TestK8sBridgeResolvesGatewayRouteKinds(t *testing.T) {
	bridge := NewK8sBridgeWithClients("default", "luna-edge", fake.NewSimpleClientset(), nil)

	bridge.storeGatewayUnstructured(&unstructured.Unstructured{Object: map[string]interface{}{
		"apiVersion": "gateway.networking.k8s.io/v1",
		"kind":       "Gateway",
		"metadata": map[string]interface{}{
			"name":      "edge",
			"namespace": "default",
		},
		"spec": map[string]interface{}{
			"listeners": []interface{}{
				map[string]interface{}{"name": "web", "protocol": "HTTP", "port": int64(80), "hostname": "app.example.com"},
				map[string]interface{}{"name": "websecure", "protocol": "HTTPS", "port": int64(443), "hostname": "secure.example.com"},
				map[string]interface{}{"name": "grpc", "protocol": "GRPC", "port": int64(8080), "hostname": "grpc.example.com"},
				map[string]interface{}{"name": "tls-pass", "protocol": "TLS", "port": int64(9443), "hostname": "tls.example.com", "tls": map[string]interface{}{"mode": "Passthrough"}},
				map[string]interface{}{"name": "tcp", "protocol": "TCP", "port": int64(9000)},
				map[string]interface{}{"name": "udp", "protocol": "UDP", "port": int64(5353)},
			},
		},
	}})

	bridge.storeHTTPRouteUnstructured(&unstructured.Unstructured{Object: map[string]interface{}{
		"apiVersion": "gateway.networking.k8s.io/v1",
		"kind":       "HTTPRoute",
		"metadata": map[string]interface{}{
			"name":      "app",
			"namespace": "default",
		},
		"spec": map[string]interface{}{
			"parentRefs": []interface{}{map[string]interface{}{"name": "edge"}},
			"hostnames":  []interface{}{"app.example.com"},
			"rules": []interface{}{map[string]interface{}{
				"matches":     []interface{}{map[string]interface{}{"path": map[string]interface{}{"type": "PathPrefix", "value": "/api"}}},
				"backendRefs": []interface{}{map[string]interface{}{"name": "svc-http", "port": int64(8080)}},
			}},
		},
	}})

	bridge.storeHTTPRouteUnstructured(&unstructured.Unstructured{Object: map[string]interface{}{
		"apiVersion": "gateway.networking.k8s.io/v1",
		"kind":       "HTTPRoute",
		"metadata": map[string]interface{}{
			"name":      "secure",
			"namespace": "default",
		},
		"spec": map[string]interface{}{
			"parentRefs": []interface{}{map[string]interface{}{"name": "edge", "sectionName": "websecure"}},
			"hostnames":  []interface{}{"secure.example.com"},
			"rules": []interface{}{map[string]interface{}{
				"backendRefs": []interface{}{map[string]interface{}{"name": "svc-https", "port": int64(8443)}},
			}},
		},
	}})

	bridge.storeGRPCRouteUnstructured(&unstructured.Unstructured{Object: map[string]interface{}{
		"apiVersion": "gateway.networking.k8s.io/v1",
		"kind":       "GRPCRoute",
		"metadata": map[string]interface{}{
			"name":      "grpc",
			"namespace": "default",
		},
		"spec": map[string]interface{}{
			"parentRefs": []interface{}{map[string]interface{}{"name": "edge"}},
			"hostnames":  []interface{}{"grpc.example.com"},
			"rules": []interface{}{map[string]interface{}{
				"backendRefs": []interface{}{map[string]interface{}{"name": "svc-grpc", "port": int64(50051)}},
			}},
		},
	}})

	bridge.storeTLSRouteUnstructured(&unstructured.Unstructured{Object: map[string]interface{}{
		"apiVersion": "gateway.networking.k8s.io/v1alpha2",
		"kind":       "TLSRoute",
		"metadata": map[string]interface{}{
			"name":      "tls",
			"namespace": "default",
		},
		"spec": map[string]interface{}{
			"parentRefs": []interface{}{map[string]interface{}{"name": "edge"}},
			"hostnames":  []interface{}{"tls.example.com"},
			"rules": []interface{}{map[string]interface{}{
				"backendRefs": []interface{}{map[string]interface{}{"name": "svc-tls", "port": int64(8443)}},
			}},
		},
	}})

	bridge.storeTCPRouteUnstructured(&unstructured.Unstructured{Object: map[string]interface{}{
		"apiVersion": "gateway.networking.k8s.io/v1alpha2",
		"kind":       "TCPRoute",
		"metadata": map[string]interface{}{
			"name":      "tcp",
			"namespace": "default",
		},
		"spec": map[string]interface{}{
			"parentRefs": []interface{}{map[string]interface{}{"name": "edge"}},
			"rules": []interface{}{map[string]interface{}{
				"backendRefs": []interface{}{map[string]interface{}{"name": "svc-tcp", "port": int64(9001)}},
			}},
		},
	}})

	bridge.storeUDPRouteUnstructured(&unstructured.Unstructured{Object: map[string]interface{}{
		"apiVersion": "gateway.networking.k8s.io/v1alpha2",
		"kind":       "UDPRoute",
		"metadata": map[string]interface{}{
			"name":      "udp",
			"namespace": "default",
		},
		"spec": map[string]interface{}{
			"parentRefs": []interface{}{map[string]interface{}{"name": "edge"}},
			"rules": []interface{}{map[string]interface{}{
				"backendRefs": []interface{}{map[string]interface{}{"name": "svc-udp", "port": int64(5353)}},
			}},
		},
	}})

	httpBackend, ok := bridge.ResolveHTTP("app.example.com", "/api/users")
	if !ok || httpBackend.Kind != metadata.ServiceBindingRouteKindHTTP || httpBackend.Binding.Name != "svc-http" {
		t.Fatalf("unexpected http backend: %#v ok=%v", httpBackend, ok)
	}

	httpsBackend, ok := bridge.ResolveHTTPS("secure.example.com", "/")
	if !ok || httpsBackend.Kind != metadata.ServiceBindingRouteKindHTTPS || httpsBackend.Binding.Name != "svc-https" {
		t.Fatalf("unexpected https backend: %#v ok=%v", httpsBackend, ok)
	}

	grpcBackend, ok := bridge.ResolveGRPC("grpc.example.com", "/")
	if !ok || grpcBackend.Kind != metadata.ServiceBindingRouteKindGRPC || grpcBackend.Binding.Name != "svc-grpc" {
		t.Fatalf("unexpected grpc backend: %#v ok=%v", grpcBackend, ok)
	}

	tlsBackend, ok := bridge.ResolveTLSPassthrough("tls.example.com")
	if !ok || tlsBackend.Kind != metadata.ServiceBindingRouteKindTLSPassthrough || tlsBackend.Binding.Name != "svc-tls" {
		t.Fatalf("unexpected tls backend: %#v ok=%v", tlsBackend, ok)
	}

	tcpBackend, ok := bridge.ResolveTCP(9000)
	if !ok || tcpBackend.Kind != metadata.ServiceBindingRouteKindTCP || tcpBackend.Binding.Name != "svc-tcp" {
		t.Fatalf("unexpected tcp backend: %#v ok=%v", tcpBackend, ok)
	}

	udpBackend, ok := bridge.ResolveUDP(5353)
	if !ok || udpBackend.Kind != metadata.ServiceBindingRouteKindUDP || udpBackend.Binding.Name != "svc-udp" {
		t.Fatalf("unexpected udp backend: %#v ok=%v", udpBackend, ok)
	}
}
