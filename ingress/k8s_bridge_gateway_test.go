package ingress

import (
	"testing"

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
	if !ok || httpBackend.Kind != RouteKindHTTP || httpBackend.Binding.Name != "svc-http" {
		t.Fatalf("unexpected http backend: %#v ok=%v", httpBackend, ok)
	}

	httpsBackend, ok := bridge.ResolveHTTPS("secure.example.com", "/")
	if !ok || httpsBackend.Kind != RouteKindHTTPS || httpsBackend.Binding.Name != "svc-https" {
		t.Fatalf("unexpected https backend: %#v ok=%v", httpsBackend, ok)
	}

	grpcBackend, ok := bridge.ResolveGRPC("grpc.example.com", "/")
	if !ok || grpcBackend.Kind != RouteKindGRPC || grpcBackend.Binding.Name != "svc-grpc" {
		t.Fatalf("unexpected grpc backend: %#v ok=%v", grpcBackend, ok)
	}

	tlsBackend, ok := bridge.ResolveTLSPassthrough("tls.example.com")
	if !ok || tlsBackend.Kind != RouteKindTLSPassthrough || tlsBackend.Binding.Name != "svc-tls" {
		t.Fatalf("unexpected tls backend: %#v ok=%v", tlsBackend, ok)
	}

	tcpBackend, ok := bridge.ResolveTCP(9000)
	if !ok || tcpBackend.Kind != RouteKindTCP || tcpBackend.Binding.Name != "svc-tcp" {
		t.Fatalf("unexpected tcp backend: %#v ok=%v", tcpBackend, ok)
	}

	udpBackend, ok := bridge.ResolveUDP(5353)
	if !ok || udpBackend.Kind != RouteKindUDP || udpBackend.Binding.Name != "svc-udp" {
		t.Fatalf("unexpected udp backend: %#v ok=%v", udpBackend, ok)
	}
}

func TestK8sBridgeTLS443Overlap(t *testing.T) {
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
				map[string]interface{}{"name": "https-app", "protocol": "HTTPS", "port": int64(443), "hostname": "app.example.com"},
				map[string]interface{}{"name": "tls-term", "protocol": "TLS", "port": int64(443), "hostname": "term.example.com", "tls": map[string]interface{}{"mode": "Terminate"}},
				map[string]interface{}{"name": "tls-pass", "protocol": "TLS", "port": int64(443), "hostname": "pass.example.com", "tls": map[string]interface{}{"mode": "Passthrough"}},
			},
		},
	}})

	bridge.storeHTTPRouteUnstructured(&unstructured.Unstructured{Object: map[string]interface{}{
		"apiVersion": "gateway.networking.k8s.io/v1",
		"kind":       "HTTPRoute",
		"metadata": map[string]interface{}{
			"name":      "https-app",
			"namespace": "default",
		},
		"spec": map[string]interface{}{
			"parentRefs": []interface{}{map[string]interface{}{"name": "edge", "sectionName": "https-app"}},
			"hostnames":  []interface{}{"app.example.com"},
			"rules": []interface{}{map[string]interface{}{
				"backendRefs": []interface{}{map[string]interface{}{"name": "svc-https-app", "port": int64(8443)}},
			}},
		},
	}})

	bridge.storeTLSRouteUnstructured(&unstructured.Unstructured{Object: map[string]interface{}{
		"apiVersion": "gateway.networking.k8s.io/v1alpha2",
		"kind":       "TLSRoute",
		"metadata": map[string]interface{}{
			"name":      "tls-term",
			"namespace": "default",
		},
		"spec": map[string]interface{}{
			"parentRefs": []interface{}{map[string]interface{}{"name": "edge", "sectionName": "tls-term"}},
			"hostnames":  []interface{}{"term.example.com"},
			"rules": []interface{}{map[string]interface{}{
				"backendRefs": []interface{}{map[string]interface{}{"name": "svc-tls-term", "port": int64(9443)}},
			}},
		},
	}})

	bridge.storeTLSRouteUnstructured(&unstructured.Unstructured{Object: map[string]interface{}{
		"apiVersion": "gateway.networking.k8s.io/v1alpha2",
		"kind":       "TLSRoute",
		"metadata": map[string]interface{}{
			"name":      "tls-pass",
			"namespace": "default",
		},
		"spec": map[string]interface{}{
			"parentRefs": []interface{}{map[string]interface{}{"name": "edge", "sectionName": "tls-pass"}},
			"hostnames":  []interface{}{"pass.example.com"},
			"rules": []interface{}{map[string]interface{}{
				"backendRefs": []interface{}{map[string]interface{}{"name": "svc-tls-pass", "port": int64(10443)}},
			}},
		},
	}})

	httpsBackend, ok := bridge.ResolveHTTPS("app.example.com", "/")
	if !ok || httpsBackend.Kind != RouteKindHTTPS || httpsBackend.Binding.Name != "svc-https-app" {
		t.Fatalf("unexpected https backend: %#v ok=%v", httpsBackend, ok)
	}

	terminatedBackend, ok := bridge.ResolveTLS("term.example.com")
	if !ok || terminatedBackend.Kind != RouteKindTLSTerminate || terminatedBackend.Binding.Name != "svc-tls-term" {
		t.Fatalf("unexpected tls termination backend: %#v ok=%v", terminatedBackend, ok)
	}

	passthroughBackend, ok := bridge.ResolveTLSPassthrough("pass.example.com")
	if !ok || passthroughBackend.Kind != RouteKindTLSPassthrough || passthroughBackend.Binding.Name != "svc-tls-pass" {
		t.Fatalf("unexpected tls passthrough backend: %#v ok=%v", passthroughBackend, ok)
	}

	if _, ok := bridge.ResolveTLS("app.example.com"); ok {
		t.Fatal("expected https sni not to match tls termination route")
	}
	if _, ok := bridge.ResolveTLSPassthrough("app.example.com"); ok {
		t.Fatal("expected https sni not to match tls passthrough route")
	}
	if _, ok := bridge.ResolveHTTPS("term.example.com", "/"); ok {
		t.Fatal("expected tls termination sni not to match https route")
	}
	if _, ok := bridge.ResolveTLSPassthrough("term.example.com"); ok {
		t.Fatal("expected tls termination sni not to match passthrough route")
	}
	if _, ok := bridge.ResolveHTTPS("pass.example.com", "/"); ok {
		t.Fatal("expected tls passthrough sni not to match https route")
	}
	if _, ok := bridge.ResolveTLS("pass.example.com"); ok {
		t.Fatal("expected tls passthrough sni not to match tls termination route")
	}
}
