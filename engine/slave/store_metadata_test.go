package slave

import (
	"context"
	"testing"
	"time"

	enginepkg "github.com/jabberwocky238/luna-edge/engine"
	"github.com/jabberwocky238/luna-edge/repository/metadata"
)

func TestLocalStoreMetadataApplySnapshotAndReadBack(t *testing.T) {
	t.Parallel()

	store, err := NewLocalStore(t.TempDir())
	if err != nil {
		t.Fatalf("new local store: %v", err)
	}
	defer func() { _ = store.Close() }()

	ctx := context.Background()
	snapshot := &enginepkg.Snapshot{
		SnapshotRecordID: 42,
		Last:             true,
		DNSRecords: []metadata.DNSRecord{{
			ID:           "dns-1",
			FQDN:         "app.example.com",
			RecordType:   metadata.DNSTypeA,
			RoutingClass: metadata.RoutingClassFirst,
			TTLSeconds:   60,
			ValuesJSON:   `["1.1.1.1"]`,
			Enabled:      true,
		}},
		DomainEntries: []metadata.DomainEntryProjection{{
			ID:          "domain-1",
			Hostname:    "app.example.com",
			BackendType: metadata.BackendTypeL7HTTP,
			Cert: &metadata.CertificateRevision{
				ID:               "cert-1",
				DomainEndpointID: "domain-1",
				Revision:         7,
				ArtifactBucket:   "bucket",
				ArtifactPrefix:   "prefix",
				SHA256Crt:        "crt",
				SHA256Key:        "key",
				NotBefore:        time.Now().Add(-time.Hour).UTC(),
				NotAfter:         time.Now().Add(time.Hour).UTC(),
			},
			HTTPRoutes: []metadata.HTTPRouteProjection{{
				ID:       "route-1",
				Path:     "/",
				Priority: 10,
				BackendRef: &metadata.ServiceBackendRef{
					ID:               "backend-1",
					ServiceNamespace: "default",
					ServiceName:      "svc-app",
					ServicePort:      8080,
				},
			}},
		}},
	}

	if err := store.ApplySnapshot(ctx, snapshot); err != nil {
		t.Fatalf("apply snapshot: %v", err)
	}

	cursor, err := store.GetSnapshotRecordID(ctx)
	if err != nil {
		t.Fatalf("get snapshot cursor: %v", err)
	}
	if cursor != 42 {
		t.Fatalf("unexpected snapshot cursor: %d", cursor)
	}

	records, err := store.ListDNSRecords(ctx)
	if err != nil {
		t.Fatalf("list dns records: %v", err)
	}
	if len(records) != 1 || records[0].ID != "dns-1" {
		t.Fatalf("unexpected dns records: %+v", records)
	}

	entry, err := store.GetDomainEntryByHostname(ctx, "app.example.com")
	if err != nil {
		t.Fatalf("get domain entry: %v", err)
	}
	if entry.Cert == nil || entry.Cert.ID != "cert-1" || entry.Cert.Revision != 7 {
		t.Fatalf("unexpected entry cert: %+v", entry.Cert)
	}

	byHost, err := store.GetDNSRecordsByHostname(ctx, "app.example.com")
	if err != nil {
		t.Fatalf("get dns records by hostname: %v", err)
	}
	if len(byHost) != 1 || byHost[0].ID != "dns-1" {
		t.Fatalf("unexpected dns records by hostname: %+v", byHost)
	}
}

func TestLocalStoreMetadataL4UsesBindedBackendRef(t *testing.T) {
	t.Parallel()

	store, err := NewLocalStore(t.TempDir())
	if err != nil {
		t.Fatalf("new local store: %v", err)
	}
	defer func() { _ = store.Close() }()

	ctx := context.Background()
	snapshot := &enginepkg.Snapshot{
		DomainEntries: []metadata.DomainEntryProjection{{
			ID:          "domain-l4",
			Hostname:    "tcp.example.com",
			BackendType: metadata.BackendTypeL4TLSPassthrough,
			BindedBackendRef: &metadata.ServiceBackendRef{
				ID:               "backend-l4",
				ServiceNamespace: "edge",
				ServiceName:      "svc-tcp",
				ServicePort:      443,
			},
		}},
	}

	if err := store.ApplySnapshot(ctx, snapshot); err != nil {
		t.Fatalf("apply snapshot: %v", err)
	}
}

func TestLocalStoreMetadataDeletesDNSRecord(t *testing.T) {
	t.Parallel()

	store, err := NewLocalStore(t.TempDir())
	if err != nil {
		t.Fatalf("new local store: %v", err)
	}
	defer func() { _ = store.Close() }()

	ctx := context.Background()
	if err := store.ApplySnapshot(ctx, &enginepkg.Snapshot{
		DNSRecords: []metadata.DNSRecord{{
			ID:         "dns-1",
			FQDN:       "app.example.com",
			RecordType: metadata.DNSTypeA,
		}},
	}); err != nil {
		t.Fatalf("seed snapshot: %v", err)
	}
	if err := store.ApplySnapshot(ctx, &enginepkg.Snapshot{
		DNSRecords: []metadata.DNSRecord{{
			Shared: metadata.Shared{Deleted: true},
			ID:     "dns-1",
			FQDN:   "app.example.com",
		}},
	}); err != nil {
		t.Fatalf("delete snapshot: %v", err)
	}

	records, err := store.GetDNSRecordsByHostname(ctx, "app.example.com")
	if err != nil {
		t.Fatalf("get dns records by hostname: %v", err)
	}
	if len(records) != 0 {
		t.Fatalf("expected dns record deleted, got %+v", records)
	}
}

func TestLocalStoreMetadataDeletesDomainEntry(t *testing.T) {
	t.Parallel()

	store, err := NewLocalStore(t.TempDir())
	if err != nil {
		t.Fatalf("new local store: %v", err)
	}
	defer func() { _ = store.Close() }()

	ctx := context.Background()
	if err := store.ApplySnapshot(ctx, &enginepkg.Snapshot{
		DomainEntries: []metadata.DomainEntryProjection{{
			ID:       "domain-1",
			Hostname: "app.example.com",
		}},
	}); err != nil {
		t.Fatalf("seed snapshot: %v", err)
	}
	if err := store.ApplySnapshot(ctx, &enginepkg.Snapshot{
		DomainEntries: []metadata.DomainEntryProjection{{
			ID:       "domain-1",
			Hostname: "app.example.com",
			Deleted:  true,
		}},
	}); err != nil {
		t.Fatalf("delete snapshot: %v", err)
	}

	entry, err := store.GetDomainEntryByHostname(ctx, "app.example.com")
	if err == nil || entry != nil {
		t.Fatalf("expected domain entry deleted, got entry=%+v err=%v", entry, err)
	}
}
