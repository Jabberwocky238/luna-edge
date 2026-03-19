package k8s_bridge

import (
	"context"
	"testing"

	"github.com/jabberwocky238/luna-edge/repository/functions"
	"github.com/jabberwocky238/luna-edge/repository/metadata"
	"gorm.io/driver/sqlite"
	"gorm.io/gorm"
	corev1 "k8s.io/api/core/v1"
	networkingv1 "k8s.io/api/networking/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/client-go/kubernetes/fake"
)

func TestIngressBridgeMaterializesExternalNameServiceAsExternalBackend(t *testing.T) {
	repo := testRepository(t)
	className := "luna-edge"
	pathType := networkingv1.PathTypePrefix
	svc := &corev1.Service{
		ObjectMeta: metav1.ObjectMeta{Name: "my-external-api", Namespace: "default"},
		Spec: corev1.ServiceSpec{
			Type:         corev1.ServiceTypeExternalName,
			ExternalName: "api.external-provider.com",
		},
	}
	ing := &networkingv1.Ingress{
		ObjectMeta: metav1.ObjectMeta{Name: "api-ingress", Namespace: "default"},
		Spec: networkingv1.IngressSpec{
			IngressClassName: &className,
			Rules: []networkingv1.IngressRule{{
				Host: "my-k8s-app.com",
				IngressRuleValue: networkingv1.IngressRuleValue{
					HTTP: &networkingv1.HTTPIngressRuleValue{
						Paths: []networkingv1.HTTPIngressPath{{
							Path:     "/external-api",
							PathType: &pathType,
							Backend: networkingv1.IngressBackend{
								Service: &networkingv1.IngressServiceBackend{
									Name: "my-external-api",
									Port: networkingv1.ServiceBackendPort{Number: 80},
								},
							},
						}},
					},
				},
			}},
		},
	}

	bridge := NewIngressBridgeWithClient("default", "luna-edge", fake.NewSimpleClientset(svc, ing), repo, func(context.Context, string) error { return nil })
	if err := bridge.LoadInitial(context.Background()); err != nil {
		t.Fatalf("LoadInitial: %v", err)
	}

	entry, err := repo.GetDomainEntryProjectionByDomain(context.Background(), "my-k8s-app.com")
	if err != nil {
		t.Fatalf("GetDomainEntryProjectionByDomain: %v", err)
	}
	if len(entry.HTTPRoutes) != 1 || entry.HTTPRoutes[0].BackendRef == nil {
		t.Fatalf("unexpected projection: %+v", entry)
	}
	backend := entry.HTTPRoutes[0].BackendRef
	if backend.Type != "EXTERNAL" || backend.ArbitraryEndpoint != "api.external-provider.com" || backend.Port != 80 {
		t.Fatalf("unexpected backend: %+v", backend)
	}
}

func testRepository(t *testing.T) functions.Repository {
	t.Helper()
	db, err := gorm.Open(sqlite.Open("file::memory:?cache=shared"), &gorm.Config{})
	if err != nil {
		t.Fatalf("open sqlite: %v", err)
	}
	if err := metadata.AutoMigrate(db); err != nil {
		t.Fatalf("auto migrate: %v", err)
	}
	return functions.NewGormRepository(db)
}
