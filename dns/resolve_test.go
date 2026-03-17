package dns

import (
	"context"
	"testing"

	"github.com/jabberwocky238/luna-edge/repository/metadata"
)

func TestResolveUsesDirectDNSLookup(t *testing.T) {
	t.Parallel()

	ctx := context.Background()
	engine := NewEngine(EngineOptions{})
	engine.RestoreRecords([]metadata.DNSRecord{{
		ID:         "dns-1",
		FQDN:       "app.example.com.",
		RecordType: "A",
		TTLSeconds: 60,
		ValuesJSON: "192.0.2.10",
		Enabled:    true,
	}, {
		ID:         "dns-2",
		FQDN:       "other.example.com.",
		RecordType: "A",
		TTLSeconds: 60,
		ValuesJSON: "192.0.2.20",
		Enabled:    true,
	}, {
		ID:         "dns-3",
		FQDN:       "app.example.com.",
		RecordType: "AAAA",
		TTLSeconds: 60,
		ValuesJSON: "2001:db8::1",
		Enabled:    true,
	}})
	result, err := engine.Lookup(ctx, DNSQuestion{FQDN: "app.example.com", RecordType: metadata.DNSTypeA})
	if err != nil {
		t.Fatalf("resolve: %v", err)
	}
	if !result.Found {
		t.Fatal("expected record to be found")
	}
	if len(result.Records) != 1 {
		t.Fatalf("expected one matching record, got %d", len(result.Records))
	}
	if result.Records[0].ID != "dns-1" {
		t.Fatalf("expected direct lookup to return dns-1, got %q", result.Records[0].ID)
	}
}

func TestResolveUsesRestoredMemoryRecords(t *testing.T) {
	t.Parallel()

	ctx := context.Background()
	engine := NewEngine(EngineOptions{})
	engine.RestoreRecords([]metadata.DNSRecord{{
		ID:         "dns-1",
		FQDN:       "app.example.com.",
		RecordType: "A",
		TTLSeconds: 60,
		ValuesJSON: "192.0.2.10",
		Enabled:    true,
	}})
	result, err := engine.Lookup(ctx, DNSQuestion{FQDN: "app.example.com", RecordType: metadata.DNSTypeA})
	if err != nil {
		t.Fatalf("first resolve: %v", err)
	}
	if len(result.Records) != 1 || result.Records[0].ValuesJSON != "192.0.2.10" {
		t.Fatalf("unexpected first resolve result: %#v", result.Records)
	}

	engine.RestoreRecords([]metadata.DNSRecord{{
		ID:         "dns-1",
		FQDN:       "app.example.com.",
		RecordType: "A",
		TTLSeconds: 60,
		ValuesJSON: "192.0.2.11",
		Enabled:    true,
	}})

	result, err = engine.Lookup(ctx, DNSQuestion{FQDN: "app.example.com", RecordType: metadata.DNSTypeA})
	if err != nil {
		t.Fatalf("second resolve: %v", err)
	}
	if len(result.Records) != 1 || result.Records[0].ValuesJSON != "192.0.2.11" {
		t.Fatalf("expected in-memory store to answer latest value, got %#v", result.Records)
	}
}
