package k8s_bridge

import (
	"context"
	"path/filepath"
	"testing"

	"github.com/jabberwocky238/luna-edge/engine/master/manage"
	"github.com/jabberwocky238/luna-edge/repository"
	"github.com/jabberwocky238/luna-edge/repository/connection"
	"github.com/jabberwocky238/luna-edge/repository/metadata"
	"k8s.io/apimachinery/pkg/apis/meta/v1/unstructured"
	"k8s.io/apimachinery/pkg/runtime"
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

	client := dynamicfake.NewSimpleDynamicClient(runtime.NewScheme())

	recorder := &effectRecorder{}
	repo := manage.NewWrapper(factory.Repository(), recorder, recorder)
	bridge := NewGatewayBridgeWithClient("default", client, repo)
	bridge.storeGateway(&unstructured.Unstructured{Object: map[string]interface{}{
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
	}})
	recorder.events = nil
	bridge.storeHTTPRoute(&unstructured.Unstructured{Object: map[string]interface{}{
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

	client := dynamicfake.NewSimpleDynamicClient(runtime.NewScheme())

	recorder := &effectRecorder{}
	repo := manage.NewWrapper(factory.Repository(), recorder, recorder)
	bridge := NewGatewayBridgeWithClient("default", client, repo)
	bridge.storeGateway(&unstructured.Unstructured{Object: map[string]interface{}{
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
	}})
	recorder.events = nil
	bridge.storeHTTPRoute(&unstructured.Unstructured{Object: map[string]interface{}{
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
	}})

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

func TestGatewayBridgeTLSRouteWritesBoundBackendAndCertPolicy(t *testing.T) {
	factory, err := repository.NewFactory(connection.Config{
		Driver:      connection.DriverSQLite,
		Path:        filepath.Join(t.TempDir(), "master.db"),
		AutoMigrate: true,
	})
	if err != nil {
		t.Fatalf("new factory: %v", err)
	}
	defer func() { _ = factory.Close() }()

	client := dynamicfake.NewSimpleDynamicClient(runtime.NewScheme())
	recorder := &effectRecorder{}
	repo := manage.NewWrapper(factory.Repository(), recorder, recorder)
	bridge := NewGatewayBridgeWithClient("default", client, repo)

	bridge.storeGateway(&unstructured.Unstructured{Object: map[string]interface{}{
		"apiVersion": "gateway.networking.k8s.io/v1",
		"kind":       "Gateway",
		"metadata": map[string]interface{}{
			"name":      "edge",
			"namespace": "default",
		},
		"spec": map[string]interface{}{
			"listeners": []interface{}{
				map[string]interface{}{"name": "tls-term", "protocol": "TLS", "port": int64(443), "hostname": "term.example.com", "tls": map[string]interface{}{"mode": "Terminate"}},
				map[string]interface{}{"name": "tls-pass", "protocol": "TLS", "port": int64(443), "hostname": "pass.example.com", "tls": map[string]interface{}{"mode": "Passthrough"}},
			},
		},
	}})
	recorder.events = nil
	bridge.storeTLSRoute(&unstructured.Unstructured{Object: map[string]interface{}{
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
				"backendRefs": []interface{}{map[string]interface{}{"name": "svc-term", "port": int64(9443)}},
			}},
		},
	}})
	if len(recorder.events) != 2 || recorder.events[0] != "cert:term.example.com" || recorder.events[1] != "publish" {
		t.Fatalf("unexpected term side effects: %+v", recorder.events)
	}
	term, err := factory.Repository().GetDomainEndpointByHostname(context.Background(), "term.example.com")
	if err != nil {
		t.Fatalf("get term domain: %v", err)
	}
	if term.BackendType != metadata.BackendTypeL4TLSTermination || !term.NeedCert || term.BindedServiceID == "" {
		t.Fatalf("unexpected term domain: %+v", term)
	}

	recorder.events = nil
	bridge.storeTLSRoute(&unstructured.Unstructured{Object: map[string]interface{}{
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
				"backendRefs": []interface{}{map[string]interface{}{"name": "svc-pass", "port": int64(10443)}},
			}},
		},
	}})
	if len(recorder.events) != 1 || recorder.events[0] != "publish" {
		t.Fatalf("unexpected pass side effects: %+v", recorder.events)
	}
	pass, err := factory.Repository().GetDomainEndpointByHostname(context.Background(), "pass.example.com")
	if err != nil {
		t.Fatalf("get pass domain: %v", err)
	}
	if pass.BackendType != metadata.BackendTypeL4TLSPassthrough || pass.NeedCert || pass.BindedServiceID == "" {
		t.Fatalf("unexpected pass domain: %+v", pass)
	}
}

func TestGatewayBridgeDeleteHTTPRouteRemovesManagedDomain(t *testing.T) {
	factory, err := repository.NewFactory(connection.Config{
		Driver:      connection.DriverSQLite,
		Path:        filepath.Join(t.TempDir(), "master.db"),
		AutoMigrate: true,
	})
	if err != nil {
		t.Fatalf("new factory: %v", err)
	}
	defer func() { _ = factory.Close() }()

	client := dynamicfake.NewSimpleDynamicClient(runtime.NewScheme())
	recorder := &effectRecorder{}
	repo := manage.NewWrapper(factory.Repository(), recorder, recorder)
	bridge := NewGatewayBridgeWithClient("default", client, repo)
	bridge.storeGateway(&unstructured.Unstructured{Object: map[string]interface{}{
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
	}})
	route := &unstructured.Unstructured{Object: map[string]interface{}{
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
	}}
	bridge.storeHTTPRoute(route)
	recorder.events = nil
	bridge.deleteHTTPRoute(route)

	if _, err := factory.Repository().GetDomainEndpointByHostname(context.Background(), "secure.example.com"); err == nil {
		t.Fatal("expected domain endpoint to be logically deleted")
	}
	if len(recorder.events) != 1 || recorder.events[0] != "publish" {
		t.Fatalf("unexpected delete side effects: %+v", recorder.events)
	}
}

func TestGatewayBridgeUpdateListenerModeRecomputesNeedCert(t *testing.T) {
	factory, err := repository.NewFactory(connection.Config{
		Driver:      connection.DriverSQLite,
		Path:        filepath.Join(t.TempDir(), "master.db"),
		AutoMigrate: true,
	})
	if err != nil {
		t.Fatalf("new factory: %v", err)
	}
	defer func() { _ = factory.Close() }()

	client := dynamicfake.NewSimpleDynamicClient(runtime.NewScheme())
	recorder := &effectRecorder{}
	repo := manage.NewWrapper(factory.Repository(), recorder, recorder)
	bridge := NewGatewayBridgeWithClient("default", client, repo)

	bridge.storeGateway(&unstructured.Unstructured{Object: map[string]interface{}{
		"apiVersion": "gateway.networking.k8s.io/v1",
		"kind":       "Gateway",
		"metadata": map[string]interface{}{
			"name":      "edge",
			"namespace": "default",
		},
		"spec": map[string]interface{}{
			"listeners": []interface{}{
				map[string]interface{}{"name": "tls-app", "protocol": "TLS", "port": int64(443), "hostname": "app.example.com", "tls": map[string]interface{}{"mode": "Passthrough"}},
			},
		},
	}})
	bridge.storeTLSRoute(&unstructured.Unstructured{Object: map[string]interface{}{
		"apiVersion": "gateway.networking.k8s.io/v1alpha2",
		"kind":       "TLSRoute",
		"metadata": map[string]interface{}{
			"name":      "tls-app",
			"namespace": "default",
		},
		"spec": map[string]interface{}{
			"parentRefs": []interface{}{map[string]interface{}{"name": "edge", "sectionName": "tls-app"}},
			"hostnames":  []interface{}{"app.example.com"},
			"rules": []interface{}{map[string]interface{}{
				"backendRefs": []interface{}{map[string]interface{}{"name": "svc-app", "port": int64(10443)}},
			}},
		},
	}})
	initial, err := factory.Repository().GetDomainEndpointByHostname(context.Background(), "app.example.com")
	if err != nil {
		t.Fatalf("get initial domain: %v", err)
	}
	if initial.NeedCert || initial.BackendType != metadata.BackendTypeL4TLSPassthrough {
		t.Fatalf("unexpected initial domain: %+v", initial)
	}

	recorder.events = nil
	bridge.storeGateway(&unstructured.Unstructured{Object: map[string]interface{}{
		"apiVersion": "gateway.networking.k8s.io/v1",
		"kind":       "Gateway",
		"metadata": map[string]interface{}{
			"name":      "edge",
			"namespace": "default",
		},
		"spec": map[string]interface{}{
			"listeners": []interface{}{
				map[string]interface{}{"name": "tls-app", "protocol": "TLS", "port": int64(443), "hostname": "app.example.com", "tls": map[string]interface{}{"mode": "Terminate"}},
			},
		},
	}})

	updated, err := factory.Repository().GetDomainEndpointByHostname(context.Background(), "app.example.com")
	if err != nil {
		t.Fatalf("get updated domain: %v", err)
	}
	if !updated.NeedCert || updated.BackendType != metadata.BackendTypeL4TLSTermination {
		t.Fatalf("unexpected updated domain: %+v", updated)
	}
	if len(recorder.events) != 2 || recorder.events[0] != "cert:app.example.com" || recorder.events[1] != "publish" {
		t.Fatalf("unexpected listener mode update side effects: %+v", recorder.events)
	}
}

func TestGatewayBridgeDeleteTLSRouteRemovesManagedDomain(t *testing.T) {
	factory, err := repository.NewFactory(connection.Config{
		Driver:      connection.DriverSQLite,
		Path:        filepath.Join(t.TempDir(), "master.db"),
		AutoMigrate: true,
	})
	if err != nil {
		t.Fatalf("new factory: %v", err)
	}
	defer func() { _ = factory.Close() }()

	client := dynamicfake.NewSimpleDynamicClient(runtime.NewScheme())
	recorder := &effectRecorder{}
	repo := manage.NewWrapper(factory.Repository(), recorder, recorder)
	bridge := NewGatewayBridgeWithClient("default", client, repo)

	bridge.storeGateway(&unstructured.Unstructured{Object: map[string]interface{}{
		"apiVersion": "gateway.networking.k8s.io/v1",
		"kind":       "Gateway",
		"metadata": map[string]interface{}{
			"name":      "edge",
			"namespace": "default",
		},
		"spec": map[string]interface{}{
			"listeners": []interface{}{
				map[string]interface{}{"name": "tls-app", "protocol": "TLS", "port": int64(443), "hostname": "app.example.com", "tls": map[string]interface{}{"mode": "Terminate"}},
			},
		},
	}})
	route := &unstructured.Unstructured{Object: map[string]interface{}{
		"apiVersion": "gateway.networking.k8s.io/v1alpha2",
		"kind":       "TLSRoute",
		"metadata": map[string]interface{}{
			"name":      "tls-app",
			"namespace": "default",
		},
		"spec": map[string]interface{}{
			"parentRefs": []interface{}{map[string]interface{}{"name": "edge", "sectionName": "tls-app"}},
			"hostnames":  []interface{}{"app.example.com"},
			"rules": []interface{}{map[string]interface{}{
				"backendRefs": []interface{}{map[string]interface{}{"name": "svc-app", "port": int64(10443)}},
			}},
		},
	}}
	bridge.storeTLSRoute(route)
	recorder.events = nil
	bridge.deleteTLSRoute(route)

	if _, err := factory.Repository().GetDomainEndpointByHostname(context.Background(), "app.example.com"); err == nil {
		t.Fatal("expected tls domain endpoint to be logically deleted")
	}
	if len(recorder.events) != 1 || recorder.events[0] != "publish" {
		t.Fatalf("unexpected tls delete side effects: %+v", recorder.events)
	}
}
