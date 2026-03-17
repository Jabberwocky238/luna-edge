package lnctl

import (
	"testing"

	"github.com/jabberwocky238/luna-edge/repository/metadata"
)

func TestValidateResource(t *testing.T) {
	t.Run("empty", func(t *testing.T) {
		if err := validateResource(""); err == nil {
			t.Fatal("expected error for empty resource")
		}
	})

	t.Run("unsupported", func(t *testing.T) {
		if err := validateResource("unknown"); err == nil {
			t.Fatal("expected error for unsupported resource")
		}
	})

	t.Run("supported", func(t *testing.T) {
		if err := validateResource("domain_endpoints"); err != nil {
			t.Fatalf("unexpected error: %v", err)
		}
	})
}

func TestValidateRequiredID(t *testing.T) {
	if err := validateRequiredID("domain_endpoints", ""); err == nil {
		t.Fatal("expected error for empty id")
	}
	if err := validateRequiredID("domain_endpoints", "domain-1"); err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
}

func TestPutZoneRequiresID(t *testing.T) {
	client := NewClient("http://127.0.0.1:8080")
	if _, err := client.DomainEndpoints().Put(metadata.DomainEndpoint{}); err == nil {
		t.Fatal("expected error for missing id")
	}
}

func TestManageResourceSupportsCurrentMetadata(t *testing.T) {
	client := NewClient("http://127.0.0.1:8080")

	for _, resource := range []string{
		"domain_endpoints",
		"service_backend_refs",
		"http_routes",
		"dns_records",
		"certificate_revisions",
		"snapshot_records",
	} {
		if _, err := client.ManageResource(resource); err != nil {
			t.Fatalf("manage resource %s: %v", resource, err)
		}
	}
}

func TestSupportedResourcesExcludeRemovedModels(t *testing.T) {
	for _, removed := range []string{
		"zones",
		"service_bindings",
		"domain_endpoint_status",
		"acme_orders",
		"acme_challenges",
		"nodes",
		"attachments",
	} {
		if isSupportedResource(removed) {
			t.Fatalf("expected removed resource %s to be unsupported", removed)
		}
	}
}
