package ingress

import (
	"context"
	"testing"

	networkingv1 "k8s.io/api/networking/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/client-go/kubernetes/fake"
)

func TestK8sBridgeMaterializesIngress(t *testing.T) {
	pathType := networkingv1.PathTypePrefix
	className := "luna-edge"
	ing := &networkingv1.Ingress{
		ObjectMeta: metav1ObjectMeta("default", "demo", 3),
		Spec: networkingv1.IngressSpec{
			IngressClassName: &className,
			Rules: []networkingv1.IngressRule{
				{
					Host: "demo.example.com",
					IngressRuleValue: networkingv1.IngressRuleValue{
						HTTP: &networkingv1.HTTPIngressRuleValue{
							Paths: []networkingv1.HTTPIngressPath{
								{
									Path:     "/",
									PathType: &pathType,
									Backend: networkingv1.IngressBackend{
										Service: &networkingv1.IngressServiceBackend{
											Name: "demo-svc",
											Port: networkingv1.ServiceBackendPort{Number: 8080},
										},
									},
								},
							},
						},
					},
				},
			},
		},
	}

	client := fake.NewSimpleClientset(ing)
	bridge := NewK8sBridgeWithClient("default", "luna-edge", client)
	if err := bridge.LoadInitial(context.Background()); err != nil {
		t.Fatalf("load initial: %v", err)
	}
	binding, route, ok := bridge.ResolveHost("demo.example.com")
	if !ok {
		t.Fatal("expected k8s bridge to resolve host")
	}
	if binding.Name != "demo-svc" || binding.Port != 8080 {
		t.Fatalf("unexpected binding: %#v", binding)
	}
	if route.Hostname != "demo.example.com" || route.RouteVersion != 3 {
		t.Fatalf("unexpected route: %#v", route)
	}
	if len(bridge.ListIngresses()) != 1 {
		t.Fatalf("expected one ingress in memory, got %d", len(bridge.ListIngresses()))
	}
}

func TestK8sBridgeResolvesLongestMatchingPath(t *testing.T) {
	prefix := networkingv1.PathTypePrefix
	exact := networkingv1.PathTypeExact
	className := "luna-edge"
	ing := &networkingv1.Ingress{
		ObjectMeta: metav1ObjectMeta("default", "demo-paths", 3),
		Spec: networkingv1.IngressSpec{
			IngressClassName: &className,
			Rules: []networkingv1.IngressRule{{
				Host: "demo.example.com",
				IngressRuleValue: networkingv1.IngressRuleValue{
					HTTP: &networkingv1.HTTPIngressRuleValue{
						Paths: []networkingv1.HTTPIngressPath{
							{
								Path:     "/",
								PathType: &prefix,
								Backend: networkingv1.IngressBackend{
									Service: &networkingv1.IngressServiceBackend{
										Name: "svc-root",
										Port: networkingv1.ServiceBackendPort{Number: 8080},
									},
								},
							},
							{
								Path:     "/api",
								PathType: &prefix,
								Backend: networkingv1.IngressBackend{
									Service: &networkingv1.IngressServiceBackend{
										Name: "svc-api",
										Port: networkingv1.ServiceBackendPort{Number: 8081},
									},
								},
							},
							{
								Path:     "/api/admin",
								PathType: &exact,
								Backend: networkingv1.IngressBackend{
									Service: &networkingv1.IngressServiceBackend{
										Name: "svc-admin",
										Port: networkingv1.ServiceBackendPort{Number: 8082},
									},
								},
							},
						},
					},
				},
			}},
		},
	}

	client := fake.NewSimpleClientset(ing)
	bridge := NewK8sBridgeWithClient("default", "luna-edge", client)
	if err := bridge.LoadInitial(context.Background()); err != nil {
		t.Fatalf("load initial: %v", err)
	}

	binding, _, ok := bridge.ResolveRequest("demo.example.com", "/api/users")
	if !ok || binding.Name != "svc-api" {
		t.Fatalf("expected /api/users to hit svc-api, got %#v ok=%v", binding, ok)
	}

	binding, _, ok = bridge.ResolveRequest("demo.example.com", "/api/admin")
	if !ok || binding.Name != "svc-admin" {
		t.Fatalf("expected exact /api/admin to hit svc-admin, got %#v ok=%v", binding, ok)
	}

	binding, _, ok = bridge.ResolveRequest("demo.example.com", "/other")
	if !ok || binding.Name != "svc-root" {
		t.Fatalf("expected /other to hit svc-root, got %#v ok=%v", binding, ok)
	}
}

func TestK8sBridgeInformerTracksAddUpdateDeleteWithFakeKube(t *testing.T) {
	client := fake.NewSimpleClientset()
	bridge := NewK8sBridgeWithClient("default", "luna-edge", client)

	pathType := networkingv1.PathTypePrefix
	className := "luna-edge"
	ing := &networkingv1.Ingress{
		ObjectMeta: metav1ObjectMeta("default", "dynamic", 1),
		Spec: networkingv1.IngressSpec{
			IngressClassName: &className,
			Rules: []networkingv1.IngressRule{{
				Host: "dynamic.example.com",
				IngressRuleValue: networkingv1.IngressRuleValue{
					HTTP: &networkingv1.HTTPIngressRuleValue{
						Paths: []networkingv1.HTTPIngressPath{{
							Path:     "/",
							PathType: &pathType,
							Backend: networkingv1.IngressBackend{
								Service: &networkingv1.IngressServiceBackend{
									Name: "svc-a",
									Port: networkingv1.ServiceBackendPort{Number: 8080},
								},
							},
						}},
					},
				},
			}},
		},
	}

	if _, err := client.NetworkingV1().Ingresses("default").Create(context.Background(), ing, metav1.CreateOptions{}); err != nil {
		t.Fatalf("create ingress: %v", err)
	}
	if err := bridge.LoadInitial(context.Background()); err != nil {
		t.Fatalf("load initial after create: %v", err)
	}
	binding, _, ok := bridge.ResolveHost("dynamic.example.com")
	if !ok || binding.Name != "svc-a" {
		t.Fatalf("expected fake kube create to materialize svc-a, got %#v, ok=%v", binding, ok)
	}

	updated := ing.DeepCopy()
	updated.Generation = 2
	updated.Spec.Rules[0].HTTP.Paths[0].Backend.Service.Name = "svc-b"
	if _, err := client.NetworkingV1().Ingresses("default").Update(context.Background(), updated, metav1.UpdateOptions{}); err != nil {
		t.Fatalf("update ingress: %v", err)
	}
	bridge.storeIngress(updated)
	binding, route, ok := bridge.ResolveHost("dynamic.example.com")
	if !ok || binding.Name != "svc-b" || route.RouteVersion != 2 {
		t.Fatalf("expected fake kube update to materialize svc-b/v2, got %#v %#v ok=%v", binding, route, ok)
	}

	if err := client.NetworkingV1().Ingresses("default").Delete(context.Background(), "dynamic", metav1.DeleteOptions{}); err != nil {
		t.Fatalf("delete ingress: %v", err)
	}
	bridge.deleteIngress(updated)
	_, _, ok = bridge.ResolveHost("dynamic.example.com")
	if ok {
		t.Fatal("expected fake kube delete to remove host from memory")
	}
}

func TestK8sBridgeFiltersByIngressClassName(t *testing.T) {
	pathType := networkingv1.PathTypePrefix
	className := "luna-edge"
	matching := &networkingv1.Ingress{
		ObjectMeta: metav1ObjectMeta("default", "match", 1),
		Spec: networkingv1.IngressSpec{
			IngressClassName: &className,
			Rules: []networkingv1.IngressRule{{
				Host: "match.example.com",
				IngressRuleValue: networkingv1.IngressRuleValue{
					HTTP: &networkingv1.HTTPIngressRuleValue{
						Paths: []networkingv1.HTTPIngressPath{{
							Path:     "/",
							PathType: &pathType,
							Backend: networkingv1.IngressBackend{
								Service: &networkingv1.IngressServiceBackend{
									Name: "svc-match",
									Port: networkingv1.ServiceBackendPort{Number: 8080},
								},
							},
						}},
					},
				},
			}},
		},
	}
	otherClass := "nginx"
	nonMatching := &networkingv1.Ingress{
		ObjectMeta: metav1ObjectMeta("default", "other", 1),
		Spec: networkingv1.IngressSpec{
			IngressClassName: &otherClass,
			Rules: []networkingv1.IngressRule{{
				Host: "other.example.com",
				IngressRuleValue: networkingv1.IngressRuleValue{
					HTTP: &networkingv1.HTTPIngressRuleValue{
						Paths: []networkingv1.HTTPIngressPath{{
							Path:     "/",
							PathType: &pathType,
							Backend: networkingv1.IngressBackend{
								Service: &networkingv1.IngressServiceBackend{
									Name: "svc-other",
									Port: networkingv1.ServiceBackendPort{Number: 8080},
								},
							},
						}},
					},
				},
			}},
		},
	}

	client := fake.NewSimpleClientset(matching, nonMatching)
	bridge := NewK8sBridgeWithClient("default", "luna-edge", client)
	if err := bridge.LoadInitial(context.Background()); err != nil {
		t.Fatalf("load initial: %v", err)
	}
	if _, _, ok := bridge.ResolveHost("match.example.com"); !ok {
		t.Fatal("expected matching ingress class to be materialized")
	}
	if _, _, ok := bridge.ResolveHost("other.example.com"); ok {
		t.Fatal("expected non-matching ingress class to be ignored")
	}
	if len(bridge.ListIngresses()) != 1 {
		t.Fatalf("expected only one ingress in memory, got %d", len(bridge.ListIngresses()))
	}
}

func TestK8sBridgeIngressClassNameOverridesAnnotation(t *testing.T) {
	pathType := networkingv1.PathTypePrefix
	className := "nginx"
	ing := &networkingv1.Ingress{
		ObjectMeta: metav1.ObjectMeta{
			Namespace:   "default",
			Name:        "override",
			Annotations: map[string]string{"kubernetes.io/ingress.class": "luna-edge"},
		},
		Spec: networkingv1.IngressSpec{
			IngressClassName: &className,
			Rules: []networkingv1.IngressRule{{
				Host: "override.example.com",
				IngressRuleValue: networkingv1.IngressRuleValue{
					HTTP: &networkingv1.HTTPIngressRuleValue{
						Paths: []networkingv1.HTTPIngressPath{{
							Path:     "/",
							PathType: &pathType,
							Backend: networkingv1.IngressBackend{
								Service: &networkingv1.IngressServiceBackend{
									Name: "svc-override",
									Port: networkingv1.ServiceBackendPort{Number: 8080},
								},
							},
						}},
					},
				},
			}},
		},
	}

	client := fake.NewSimpleClientset(ing)
	bridge := NewK8sBridgeWithClient("default", "luna-edge", client)
	if err := bridge.LoadInitial(context.Background()); err != nil {
		t.Fatalf("load initial: %v", err)
	}
	if _, _, ok := bridge.ResolveHost("override.example.com"); ok {
		t.Fatal("expected spec.ingressClassName to take precedence over annotation")
	}
}
