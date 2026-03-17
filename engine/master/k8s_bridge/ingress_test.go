package k8s_bridge

import (
	"context"
	"path/filepath"
	"testing"

	"github.com/jabberwocky238/luna-edge/repository"
	"github.com/jabberwocky238/luna-edge/repository/connection"
	"github.com/jabberwocky238/luna-edge/repository/metadata"
	networkingv1 "k8s.io/api/networking/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/client-go/kubernetes/fake"
)

type effectRecorder struct {
	events []string
}

func (r *effectRecorder) NotifyCertificateDesired(_ context.Context, fqdn string) error {
	r.events = append(r.events, "cert:"+fqdn)
	return nil
}

func (r *effectRecorder) PublishNode(_ context.Context, _ string) error {
	r.events = append(r.events, "publish")
	return nil
}

func TestIngressBridgeWritesMasterThenCertThenBroadcast(t *testing.T) {
	factory, err := repository.NewFactory(connection.Config{
		Driver:      connection.DriverSQLite,
		Path:        filepath.Join(t.TempDir(), "master.db"),
		AutoMigrate: true,
	})
	if err != nil {
		t.Fatalf("new factory: %v", err)
	}
	defer func() { _ = factory.Close() }()

	pathType := networkingv1.PathTypePrefix
	className := "luna-edge"
	client := fake.NewSimpleClientset(&networkingv1.Ingress{
		ObjectMeta: metav1.ObjectMeta{
			Namespace: "default",
			Name:      "demo",
		},
		Spec: networkingv1.IngressSpec{
			IngressClassName: &className,
			TLS: []networkingv1.IngressTLS{{
				Hosts: []string{"app.example.com"},
			}},
			Rules: []networkingv1.IngressRule{{
				Host: "app.example.com",
				IngressRuleValue: networkingv1.IngressRuleValue{
					HTTP: &networkingv1.HTTPIngressRuleValue{
						Paths: []networkingv1.HTTPIngressPath{{
							Path:     "/",
							PathType: &pathType,
							Backend: networkingv1.IngressBackend{
								Service: &networkingv1.IngressServiceBackend{
									Name: "svc-app",
									Port: networkingv1.ServiceBackendPort{Number: 8080},
								},
							},
						}},
					},
				},
			}},
		},
	})

	recorder := &effectRecorder{}
	bridge := NewIngressBridgeWithClient("default", "luna-edge", client, factory.Repository(), recorder)
	if err := bridge.LoadInitial(context.Background()); err != nil {
		t.Fatalf("load initial: %v", err)
	}

	domain, err := factory.Repository().GetDomainEndpointByHostname(context.Background(), "app.example.com")
	if err != nil {
		t.Fatalf("get domain: %v", err)
	}
	if !domain.NeedCert || domain.BackendType != "l7-https" {
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
	if backend.ServiceNamespace != "default" || backend.ServiceName != "svc-app" || backend.ServicePort != 8080 {
		t.Fatalf("unexpected backend ref: %+v", backend)
	}
	if len(recorder.events) != 2 || recorder.events[0] != "cert:app.example.com" || recorder.events[1] != "publish" {
		t.Fatalf("unexpected side effect order: %+v", recorder.events)
	}
}

func TestIngressBridgeUpdateRebuildsAffectedDomain(t *testing.T) {
	factory, err := repository.NewFactory(connection.Config{
		Driver:      connection.DriverSQLite,
		Path:        filepath.Join(t.TempDir(), "master.db"),
		AutoMigrate: true,
	})
	if err != nil {
		t.Fatalf("new factory: %v", err)
	}
	defer func() { _ = factory.Close() }()

	pathType := networkingv1.PathTypePrefix
	className := "luna-edge"
	recorder := &effectRecorder{}
	bridge := NewIngressBridgeWithClient("default", "luna-edge", fake.NewSimpleClientset(), factory.Repository(), recorder)

	bridge.storeIngress(&networkingv1.Ingress{
		ObjectMeta: metav1.ObjectMeta{
			Namespace: "default",
			Name:      "demo",
		},
		Spec: networkingv1.IngressSpec{
			IngressClassName: &className,
			TLS: []networkingv1.IngressTLS{{Hosts: []string{"app.example.com"}}},
			Rules: []networkingv1.IngressRule{{
				Host: "app.example.com",
				IngressRuleValue: networkingv1.IngressRuleValue{
					HTTP: &networkingv1.HTTPIngressRuleValue{
						Paths: []networkingv1.HTTPIngressPath{{
							Path:     "/",
							PathType: &pathType,
							Backend: networkingv1.IngressBackend{
								Service: &networkingv1.IngressServiceBackend{
									Name: "svc-v1",
									Port: networkingv1.ServiceBackendPort{Number: 8080},
								},
							},
						}},
					},
				},
			}},
		},
	})

	recorder.events = nil
	bridge.storeIngress(&networkingv1.Ingress{
		ObjectMeta: metav1.ObjectMeta{
			Namespace: "default",
			Name:      "demo",
		},
		Spec: networkingv1.IngressSpec{
			IngressClassName: &className,
			TLS: []networkingv1.IngressTLS{{Hosts: []string{"app.example.com"}}},
			Rules: []networkingv1.IngressRule{{
				Host: "app.example.com",
				IngressRuleValue: networkingv1.IngressRuleValue{
					HTTP: &networkingv1.HTTPIngressRuleValue{
						Paths: []networkingv1.HTTPIngressPath{{
							Path:     "/api",
							PathType: &pathType,
							Backend: networkingv1.IngressBackend{
								Service: &networkingv1.IngressServiceBackend{
									Name: "svc-v2",
									Port: networkingv1.ServiceBackendPort{Number: 9090},
								},
							},
						}},
					},
				},
			}},
		},
	})

	domain, err := factory.Repository().GetDomainEndpointByHostname(context.Background(), "app.example.com")
	if err != nil {
		t.Fatalf("get domain: %v", err)
	}
	routes, err := factory.Repository().ListHTTPRoutesByDomainID(context.Background(), domain.ID)
	if err != nil {
		t.Fatalf("list routes: %v", err)
	}
	if len(routes) != 1 || routes[0].Path != "/api" {
		t.Fatalf("expected updated route set, got %+v", routes)
	}
	backend := &metadata.ServiceBackendRef{}
	if err := factory.Repository().ServiceBindingRefs().GetResourceByField(context.Background(), backend, "id", routes[0].BackendRefID); err != nil {
		t.Fatalf("get backend: %v", err)
	}
	if backend.ServiceName != "svc-v2" || backend.ServicePort != 9090 {
		t.Fatalf("expected updated backend, got %+v", backend)
	}
	if len(recorder.events) != 2 || recorder.events[0] != "cert:app.example.com" || recorder.events[1] != "publish" {
		t.Fatalf("unexpected side effects after update: %+v", recorder.events)
	}
}

func TestIngressBridgeDeleteRemovesManagedDomain(t *testing.T) {
	factory, err := repository.NewFactory(connection.Config{
		Driver:      connection.DriverSQLite,
		Path:        filepath.Join(t.TempDir(), "master.db"),
		AutoMigrate: true,
	})
	if err != nil {
		t.Fatalf("new factory: %v", err)
	}
	defer func() { _ = factory.Close() }()

	pathType := networkingv1.PathTypePrefix
	className := "luna-edge"
	recorder := &effectRecorder{}
	bridge := NewIngressBridgeWithClient("default", "luna-edge", fake.NewSimpleClientset(), factory.Repository(), recorder)

	ing := &networkingv1.Ingress{
		ObjectMeta: metav1.ObjectMeta{
			Namespace: "default",
			Name:      "demo",
		},
		Spec: networkingv1.IngressSpec{
			IngressClassName: &className,
			TLS: []networkingv1.IngressTLS{{Hosts: []string{"app.example.com"}}},
			Rules: []networkingv1.IngressRule{{
				Host: "app.example.com",
				IngressRuleValue: networkingv1.IngressRuleValue{
					HTTP: &networkingv1.HTTPIngressRuleValue{
						Paths: []networkingv1.HTTPIngressPath{{
							Path:     "/",
							PathType: &pathType,
							Backend: networkingv1.IngressBackend{
								Service: &networkingv1.IngressServiceBackend{
									Name: "svc-app",
									Port: networkingv1.ServiceBackendPort{Number: 8080},
								},
							},
						}},
					},
				},
			}},
		},
	}
	bridge.storeIngress(ing)
	recorder.events = nil
	bridge.deleteIngress(ing)

	if _, err := factory.Repository().GetDomainEndpointByHostname(context.Background(), "app.example.com"); err == nil {
		t.Fatal("expected domain endpoint to be logically deleted")
	}
	if len(recorder.events) != 1 || recorder.events[0] != "publish" {
		t.Fatalf("unexpected side effects after delete: %+v", recorder.events)
	}
}
