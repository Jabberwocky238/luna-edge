package k8s_bridge

import (
	"context"
	"path/filepath"
	"testing"

	"github.com/jabberwocky238/luna-edge/repository"
	"github.com/jabberwocky238/luna-edge/repository/connection"
	"github.com/jabberwocky238/luna-edge/repository/metadata"
	"k8s.io/apimachinery/pkg/apis/meta/v1/unstructured"
	"k8s.io/apimachinery/pkg/runtime"
	"k8s.io/apimachinery/pkg/runtime/schema"
	dynamicfake "k8s.io/client-go/dynamic/fake"
)

func TestGatewayBridgeWritesMasterThenCertThenBroadcast(t *testing.T) {
	factory, err := repository.NewFactory(connection.Config{
		Driver:      connection.DriverSQLite,
		Path:        filepath.Join(t.TempDir(), "master.db"),
		AutoMigrate: true,
	})
	if err != nil {
		t.Fatalf("new factory: %v", err)
	}
	defer func() { _ = factory.Close() }()

	client := dynamicfake.NewSimpleDynamicClientWithCustomListKinds(
		runtime.NewScheme(),
		map[schema.GroupVersionResource]string{
			gatewayGVR:   "GatewayList",
			httpRouteGVR: "HTTPRouteList",
		},
		&unstructured.Unstructured{Object: map[string]interface{}{
			"apiVersion": "gateway.networking.k8s.io/v1",
			"kind":       "Gateway",
			"metadata": map[string]interface{}{
				"name":      "edge",
				"namespace": "default",
			},
			"spec": map[string]interface{}{
				"listeners": []interface{}{
					map[string]interface{}{"name": "websecure", "protocol": "HTTPS", "port": int64(443), "hostname": "secure.example.com"},
				},
			},
		}},
		&unstructured.Unstructured{Object: map[string]interface{}{
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
		}},
	)

	recorder := &effectRecorder{}
	bridge := NewGatewayBridgeWithClient("default", client, factory.Repository(), recorder)
	if err := bridge.LoadInitial(context.Background()); err != nil {
		t.Fatalf("load initial: %v", err)
	}

	domain, err := factory.Repository().GetDomainEndpointByHostname(context.Background(), "secure.example.com")
	if err != nil {
		t.Fatalf("get domain: %v", err)
	}
	if !domain.NeedCert || domain.BackendType != metadata.BackendTypeL7HTTPS {
		t.Fatalf("unexpected domain endpoint: %+v", domain)
	}
	routes, err := factory.Repository().ListHTTPRoutesByDomainID(context.Background(), domain.ID)
	if err != nil {
		t.Fatalf("list routes: %v", err)
	}
	if len(routes) != 1 || routes[0].BackendRefID == "" {
		t.Fatalf("expected one route, got %+v", routes)
	}
	backend := &metadata.ServiceBackendRef{}
	if err := factory.Repository().ServiceBindingRefs().GetResourceByField(context.Background(), backend, "id", routes[0].BackendRefID); err != nil {
		t.Fatalf("get backend: %v", err)
	}
	if backend.ServiceNamespace != "default" || backend.ServiceName != "svc-https" || backend.ServicePort != 8443 {
		t.Fatalf("unexpected backend ref: %+v", backend)
	}
	if len(recorder.events) != 2 || recorder.events[0] != "cert:secure.example.com" || recorder.events[1] != "publish" {
		t.Fatalf("unexpected side effect order: %+v", recorder.events)
	}
}

func TestGatewayBridgeIgnoresHTTPListenerForCertificateRequest(t *testing.T) {
	factory, err := repository.NewFactory(connection.Config{
		Driver:      connection.DriverSQLite,
		Path:        filepath.Join(t.TempDir(), "master.db"),
		AutoMigrate: true,
	})
	if err != nil {
		t.Fatalf("new factory: %v", err)
	}
	defer func() { _ = factory.Close() }()

	client := dynamicfake.NewSimpleDynamicClientWithCustomListKinds(
		runtime.NewScheme(),
		map[schema.GroupVersionResource]string{
			gatewayGVR:   "GatewayList",
			httpRouteGVR: "HTTPRouteList",
		},
		&unstructured.Unstructured{Object: map[string]interface{}{
			"apiVersion": "gateway.networking.k8s.io/v1",
			"kind":       "Gateway",
			"metadata": map[string]interface{}{
				"name":      "edge",
				"namespace": "default",
			},
			"spec": map[string]interface{}{
				"listeners": []interface{}{
					map[string]interface{}{"name": "web", "protocol": "HTTP", "port": int64(80), "hostname": "plain.example.com"},
				},
			},
		}},
		&unstructured.Unstructured{Object: map[string]interface{}{
			"apiVersion": "gateway.networking.k8s.io/v1",
			"kind":       "HTTPRoute",
			"metadata": map[string]interface{}{
				"name":      "plain",
				"namespace": "default",
			},
			"spec": map[string]interface{}{
				"parentRefs": []interface{}{map[string]interface{}{"name": "edge", "sectionName": "web"}},
				"hostnames":  []interface{}{"plain.example.com"},
				"rules": []interface{}{map[string]interface{}{
					"backendRefs": []interface{}{map[string]interface{}{"name": "svc-http", "port": int64(8080)}},
				}},
			},
		}},
	)

	recorder := &effectRecorder{}
	bridge := NewGatewayBridgeWithClient("default", client, factory.Repository(), recorder)
	if err := bridge.LoadInitial(context.Background()); err != nil {
		t.Fatalf("load initial: %v", err)
	}

	domain, err := factory.Repository().GetDomainEndpointByHostname(context.Background(), "plain.example.com")
	if err != nil {
		t.Fatalf("get domain: %v", err)
	}
	if domain.NeedCert {
		t.Fatalf("expected no cert request for http listener, got %+v", domain)
	}
	if len(recorder.events) != 1 || recorder.events[0] != "publish" {
		t.Fatalf("unexpected side effects: %+v", recorder.events)
	}
}
