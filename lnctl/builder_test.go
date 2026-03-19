package lnctl

import (
	"testing"

	"github.com/jabberwocky238/luna-edge/repository/metadata"
)

func TestBuilderBuildL7Plan(t *testing.T) {
	plan, err := NewBuilder("app.example.com").
		AsL7HTTPS().
		Route("/", BackendTarget{
			Type:             metadata.ServiceBackendTypeSVC,
			ServiceNamespace: "default",
			ServiceName:      "frontend",
			Port:             443,
		}).
		Route("/api", BackendTarget{
			Type:              metadata.ServiceBackendTypeExternal,
			ArbitraryEndpoint: "api.example.net",
			Port:              8443,
		}).
		WantDNS(metadata.DNSRecord{
			FQDN:         "app.example.com",
			RecordType:   metadata.DNSTypeA,
			RoutingClass: metadata.RoutingClassFirst,
			TTLSeconds:   60,
			ValuesJSON:   `["1.2.3.4"]`,
			Enabled:      true,
		}).
		Build()
	if err != nil {
		t.Fatalf("Build: %v", err)
	}

	if len(plan.DomainEndpoints) != 1 || plan.DomainEndpoints[0].Action != PlanActionCreate {
		t.Fatalf("unexpected domain plan: %+v", plan.DomainEndpoints)
	}
	if len(plan.ServiceBackendRefs) != 2 {
		t.Fatalf("unexpected backend ref plan: %+v", plan.ServiceBackendRefs)
	}
	if len(plan.HTTPRoutes) != 2 {
		t.Fatalf("unexpected route plan: %+v", plan.HTTPRoutes)
	}
	if len(plan.DNSRecords) != 1 || plan.DNSRecords[0].Action != PlanActionCreate {
		t.Fatalf("unexpected dns plan: %+v", plan.DNSRecords)
	}
	if plan.ServiceBackendRefs[1].Desired == nil || plan.ServiceBackendRefs[1].Desired.Type != metadata.ServiceBackendTypeSVC {
		t.Fatalf("expected stable backend ordering and svc route, got: %+v", plan.ServiceBackendRefs)
	}
}

func TestBuilderBuildUpdatesExistingProjection(t *testing.T) {
	existing := &metadata.DomainEntryProjection{
		ID:          "domain:app.example.com",
		Hostname:    "app.example.com",
		BackendType: metadata.BackendTypeL7HTTP,
		HTTPRoutes: []metadata.HTTPRouteProjection{
			{
				ID:       "route:app.example.com:root",
				Path:     "/",
				Priority: 1,
				BackendRef: &metadata.ServiceBackendRef{
					ID:               "backend:app.example.com:root",
					Type:             metadata.ServiceBackendTypeSVC,
					ServiceNamespace: "default",
					ServiceName:      "old",
					Port:             80,
				},
			},
		},
	}

	plan, err := NewBuilder("app.example.com").
		WithExistingProjection(existing).
		WithExistingDNSRecords(metadata.DNSRecord{
			ID:           "dns:app_example_com:a",
			FQDN:         "app.example.com",
			RecordType:   metadata.DNSTypeA,
			RoutingClass: metadata.RoutingClassFirst,
			TTLSeconds:   60,
			ValuesJSON:   `["1.1.1.1"]`,
			Enabled:      true,
		}).
		AsL7HTTPBoth().
		Route("/", BackendTarget{
			Type:             metadata.ServiceBackendTypeSVC,
			ServiceNamespace: "default",
			ServiceName:      "new",
			Port:             8080,
		}).
		WantDNS(metadata.DNSRecord{
			ID:           "dns:app_example_com:a",
			FQDN:         "app.example.com",
			RecordType:   metadata.DNSTypeA,
			RoutingClass: metadata.RoutingClassFirst,
			TTLSeconds:   60,
			ValuesJSON:   `["2.2.2.2"]`,
			Enabled:      true,
		}).
		Build()
	if err != nil {
		t.Fatalf("Build: %v", err)
	}

	if len(plan.DomainEndpoints) != 1 || plan.DomainEndpoints[0].Action != PlanActionUpdate {
		t.Fatalf("unexpected domain diff: %+v", plan.DomainEndpoints)
	}
	if len(plan.ServiceBackendRefs) != 1 || plan.ServiceBackendRefs[0].Action != PlanActionUpdate {
		t.Fatalf("unexpected backend diff: %+v", plan.ServiceBackendRefs)
	}
	if len(plan.HTTPRoutes) != 0 {
		t.Fatalf("unexpected route diff: %+v", plan.HTTPRoutes)
	}
	if len(plan.DNSRecords) != 1 || plan.DNSRecords[0].Action != PlanActionUpdate {
		t.Fatalf("unexpected dns diff: %+v", plan.DNSRecords)
	}
}

func TestBuilderBuildL4DeletesOldL7Routes(t *testing.T) {
	existing := &metadata.DomainEntryProjection{
		ID:          "domain:tcp.example.com",
		Hostname:    "tcp.example.com",
		BackendType: metadata.BackendTypeL7HTTP,
		HTTPRoutes: []metadata.HTTPRouteProjection{
			{
				ID:       "route:tcp.example.com:root",
				Path:     "/",
				Priority: 1,
				BackendRef: &metadata.ServiceBackendRef{
					ID:               "backend:tcp.example.com:root",
					Type:             metadata.ServiceBackendTypeSVC,
					ServiceNamespace: "default",
					ServiceName:      "legacy",
					Port:             80,
				},
			},
		},
	}

	plan, err := NewBuilder("tcp.example.com").
		WithExistingProjection(existing).
		AsL4TLSPassthrough(BackendTarget{
			Type:             metadata.ServiceBackendTypeSVC,
			ServiceNamespace: "default",
			ServiceName:      "stream",
			Port:             443,
		}).
		Build()
	if err != nil {
		t.Fatalf("Build: %v", err)
	}

	if len(plan.DomainEndpoints) != 1 || plan.DomainEndpoints[0].Action != PlanActionUpdate {
		t.Fatalf("unexpected domain diff: %+v", plan.DomainEndpoints)
	}
	if len(plan.ServiceBackendRefs) != 2 {
		t.Fatalf("unexpected backend diff: %+v", plan.ServiceBackendRefs)
	}
	if len(plan.HTTPRoutes) != 1 || plan.HTTPRoutes[0].Action != PlanActionDelete {
		t.Fatalf("unexpected route diff: %+v", plan.HTTPRoutes)
	}
}
