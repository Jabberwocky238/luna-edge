package lnctl

import (
	"path/filepath"
	"testing"

	"github.com/jabberwocky238/luna-edge/repository/metadata"
)

func TestMockClientPersistsInputsAndQueriesState(t *testing.T) {
	rawClient := NewMockClient("sqlite://?mode=memory")
	client, ok := rawClient.(*MockClient)
	if !ok {
		t.Fatalf("NewMockClient returned %T, want *MockClient", rawClient)
	}
	defer client.Close()

	err := client.SeedDomainEntryProjection(&metadata.DomainEntryProjection{
		Hostname:    "app.example.com",
		NeedCert:    true,
		BackendType: metadata.BackendTypeL7HTTPS,
		HTTPRoutes: []metadata.HTTPRouteProjection{
			{
				ID:       "route:app:root",
				Path:     "/",
				Priority: 1,
				BackendRef: &metadata.ServiceBackendRef{
					ID:               "backend:app:root",
					Type:             metadata.ServiceBackendTypeSVC,
					ServiceNamespace: "default",
					ServiceName:      "frontend",
					Port:             443,
				},
			},
		},
	})
	if err != nil {
		t.Fatalf("SeedDomainEntryProjection: %v", err)
	}
	err = client.SeedDNSRecords(metadata.DNSRecord{
		ID:           "dns:app:a",
		FQDN:         "app.example.com",
		RecordType:   metadata.DNSTypeA,
		RoutingClass: metadata.RoutingClassFirst,
		TTLSeconds:   60,
		ValuesJSON:   `["1.2.3.4"]`,
		Enabled:      true,
	})
	if err != nil {
		t.Fatalf("SeedDNSRecords: %v", err)
	}

	projection, err := client.QueryDomainEntryProjection("app.example.com")
	if err != nil {
		t.Fatalf("QueryDomainEntryProjection: %v", err)
	}
	if projection.Hostname != "app.example.com" || len(projection.HTTPRoutes) != 1 {
		t.Fatalf("unexpected projection: %+v", projection)
	}

	records, err := client.QueryDNSRecords("app.example.com", "A")
	if err != nil {
		t.Fatalf("QueryDNSRecords: %v", err)
	}
	if len(records) != 1 || records[0].ID != "dns:app:a" {
		t.Fatalf("unexpected records: %+v", records)
	}

	inputs, err := client.Inputs()
	if err != nil {
		t.Fatalf("Inputs: %v", err)
	}
	if len(inputs) != 2 {
		t.Fatalf("expected 2 saved inputs, got %d", len(inputs))
	}
	if inputs[0].Kind != "query_domain_entry_projection" || inputs[1].Kind != "query_dns_records" {
		t.Fatalf("unexpected saved inputs: %+v", inputs)
	}
}

func TestMockClientApplyPlanUpdatesStateAndSupportsFileSQLiteURL(t *testing.T) {
	dbPath := filepath.Join(t.TempDir(), "mock-client.db")
	rawClient := NewMockClient("sqlite://" + filepath.ToSlash(dbPath))
	client, ok := rawClient.(*MockClient)
	if !ok {
		t.Fatalf("NewMockClient returned %T, want *MockClient", rawClient)
	}
	defer client.Close()

	plan := &Plan{
		Hostname: "api.example.com",
		DomainEndpoints: []DomainEndpointChange{
			{
				Action: PlanActionCreate,
				Desired: &metadata.DomainEndpoint{
					Hostname:    "api.example.com",
					NeedCert:    true,
					BackendType: metadata.BackendTypeL7HTTPS,
				},
			},
		},
		ServiceBackendRefs: []ServiceBackendRefChange{
			{
				Action: PlanActionCreate,
				Desired: &metadata.ServiceBackendRef{
					ID:               "backend:api:root",
					Type:             metadata.ServiceBackendTypeSVC,
					ServiceNamespace: "default",
					ServiceName:      "api",
					Port:             8443,
				},
			},
		},
		HTTPRoutes: []HTTPRouteChange{
			{
				Action: PlanActionCreate,
				Desired: &metadata.HTTPRoute{
					ID:           "route:api:root",
					Hostname:     "api.example.com",
					Path:         "/",
					Priority:     1,
					BackendRefID: "backend:api:root",
				},
			},
		},
		DNSRecords: []DNSRecordChange{
			{
				Action: PlanActionCreate,
				Desired: &metadata.DNSRecord{
					ID:           "dns:api:a",
					FQDN:         "api.example.com",
					RecordType:   metadata.DNSTypeA,
					RoutingClass: metadata.RoutingClassFirst,
					TTLSeconds:   60,
					ValuesJSON:   `["10.0.0.8"]`,
					Enabled:      true,
				},
			},
		},
	}

	applied, err := client.ApplyPlan(plan)
	if err != nil {
		t.Fatalf("ApplyPlan: %v", err)
	}
	if applied.Hostname != "api.example.com" {
		t.Fatalf("unexpected applied plan: %+v", applied)
	}

	projection, err := client.QueryDomainEntryProjection("api.example.com")
	if err != nil {
		t.Fatalf("QueryDomainEntryProjection: %v", err)
	}
	if projection.BackendType != metadata.BackendTypeL7HTTPS || len(projection.HTTPRoutes) != 1 {
		t.Fatalf("unexpected projection after apply: %+v", projection)
	}

	records, err := client.QueryDNSRecords("api.example.com", "A")
	if err != nil {
		t.Fatalf("QueryDNSRecords: %v", err)
	}
	if len(records) != 1 || records[0].ValuesJSON != `["10.0.0.8"]` {
		t.Fatalf("unexpected dns records after apply: %+v", records)
	}

	inputs, err := client.Inputs()
	if err != nil {
		t.Fatalf("Inputs: %v", err)
	}
	if len(inputs) != 3 {
		t.Fatalf("expected 3 saved inputs, got %d", len(inputs))
	}
	if inputs[0].Kind != "apply_plan" {
		t.Fatalf("expected apply_plan to be recorded first, got %+v", inputs[0])
	}
}
