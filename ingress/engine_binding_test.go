package ingress

import (
	"testing"

	"github.com/jabberwocky238/luna-edge/repository/metadata"
)

func TestBindingFromProjection_ServiceBackendAddressing(t *testing.T) {
	entry := &metadata.DomainEntryProjection{
		Hostname: "svc.example.com",
		HTTPRoutes: []metadata.HTTPRouteProjection{{
			ID:       "route-1",
			Path:     "/",
			Priority: 1,
			BackendRef: &metadata.ServiceBackendRef{
				ID:               "backend-1",
				Type:             metadata.ServiceBackendTypeSVC,
				ServiceNamespace: "default",
				ServiceName:      "web",
				Port:             8080,
			},
		}},
	}

	got := bindingFromProjection(entry, "/", RouteKindHTTP)
	if got == nil {
		t.Fatal("expected binding")
	}
	if got.Address != "web.default.svc.cluster.local" || got.Port != 8080 {
		t.Fatalf("unexpected svc binding: %+v", got)
	}
}

func TestBindingFromProjection_ExternalBackendAddressing(t *testing.T) {
	entry := &metadata.DomainEntryProjection{
		Hostname: "ext.example.com",
		HTTPRoutes: []metadata.HTTPRouteProjection{{
			ID:       "route-ext",
			Path:     "/",
			Priority: 1,
			BackendRef: &metadata.ServiceBackendRef{
				ID:                "backend-ext",
				Type:              metadata.ServiceBackendTypeExternal,
				ArbitraryEndpoint: "127.0.0.1",
				Port:              18443,
			},
		}},
	}

	got := bindingFromProjection(entry, "/", RouteKindHTTPS)
	if got == nil {
		t.Fatal("expected binding")
	}
	if got.Address != "127.0.0.1" || got.Port != 18443 {
		t.Fatalf("unexpected external binding: %+v", got)
	}
}
