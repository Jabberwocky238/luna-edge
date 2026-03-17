package master

import (
	"context"
	"net"
	"path/filepath"
	"testing"

	"github.com/jabberwocky238/luna-edge/engine/master/manage"
	"github.com/jabberwocky238/luna-edge/repository"
	"github.com/jabberwocky238/luna-edge/repository/connection"
	"github.com/jabberwocky238/luna-edge/repository/metadata"
)

func TestEngineStartReturnsManageListenError(t *testing.T) {
	t.Parallel()

	lis, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatalf("listen: %v", err)
	}
	defer func() { _ = lis.Close() }()

	eng := &Engine{
		Config: Config{
			ManageListenAddr: lis.Addr().String(),
		},
		Manage: manage.NewAPI(nil),
	}

	if err := eng.Start(); err == nil {
		t.Fatal("expected start to fail when manage port is already in use")
	}
	_ = eng.Stop(context.Background())
}

func TestBuildSnapshotIncludesDNSRecordsAndDomainEntries(t *testing.T) {
	t.Parallel()

	factory, err := repository.NewFactory(connection.Config{
		Driver:      connection.DriverSQLite,
		Path:        filepath.Join(t.TempDir(), "master.db"),
		AutoMigrate: true,
	})
	if err != nil {
		t.Fatalf("new factory: %v", err)
	}
	defer func() { _ = factory.Close() }()

	repo := factory.Repository()
	ctx := context.Background()
	if err := repo.DomainEndpoints().UpsertResource(ctx, &metadata.DomainEndpoint{
		ID:          "domain-1",
		Hostname:    "app.example.com",
		BackendType: metadata.BackendTypeL7HTTP,
		CertID:      "cert-1",
	}); err != nil {
		t.Fatalf("upsert domain: %v", err)
	}
	if err := repo.ServiceBindingRefs().UpsertResource(ctx, &metadata.ServiceBackendRef{
		ID:               "backend-1",
		ServiceNamespace: "default",
		ServiceName:      "svc-app",
		ServicePort:      8080,
	}); err != nil {
		t.Fatalf("upsert backend ref: %v", err)
	}
	if err := repo.HTTPRoutes().UpsertResource(ctx, &metadata.HTTPRoute{
		ID:               "route-1",
		DomainEndpointID: "domain-1",
		Hostname:         "app.example.com",
		Path:             "/",
		Priority:         10,
		BackendRefID:     "backend-1",
	}); err != nil {
		t.Fatalf("upsert route: %v", err)
	}
	if err := repo.DNSRecords().UpsertResource(ctx, &metadata.DNSRecord{
		ID:           "dns-1",
		FQDN:         "app.example.com",
		RecordType:   metadata.DNSTypeA,
		RoutingClass: metadata.RoutingClassFirst,
		TTLSeconds:   60,
		ValuesJSON:   `["1.1.1.1"]`,
		Enabled:      true,
	}); err != nil {
		t.Fatalf("upsert dns: %v", err)
	}
	if err := repo.CertificateRevisions().UpsertResource(ctx, &metadata.CertificateRevision{
		ID:               "cert-1",
		DomainEndpointID: "domain-1",
		Hostname:         "app.example.com",
		Revision:         9,
	}); err != nil {
		t.Fatalf("upsert cert: %v", err)
	}
	eng := &Engine{
		Factory: factory,
		Repo:    repo,
		Hub:     NewHub(),
	}

	snapshot, err := eng.BuildSnapshot(ctx, "node-1")
	if err != nil {
		t.Fatalf("build snapshot: %v", err)
	}
	if len(snapshot.DNSRecords) != 1 {
		t.Fatalf("unexpected dns records: %+v", snapshot.DNSRecords)
	}
	if len(snapshot.DomainEntries) != 1 {
		t.Fatalf("unexpected domain entries: %+v", snapshot.DomainEntries)
	}
	if snapshot.DomainEntries[0].Hostname != "app.example.com" {
		t.Fatalf("unexpected domain entry: %+v", snapshot.DomainEntries[0])
	}
	if snapshot.DomainEntries[0].Cert == nil || snapshot.DomainEntries[0].Cert.Revision != 9 {
		t.Fatalf("unexpected domain entry cert: %+v", snapshot.DomainEntries[0].Cert)
	}
}

func TestNewConfiguresS3BundleProviderWhenEnabled(t *testing.T) {
	t.Parallel()

	eng, err := New(Config{
		StorageDriver: connection.DriverSQLite,
		SQLitePath:    filepath.Join(t.TempDir(), "master.db"),
		AutoMigrate:   true,
		S3: S3Config{
			Endpoint:        "http://127.0.0.1:9000",
			Region:          "us-east-1",
			AccessKeyID:     "test-access",
			SecretAccessKey: "test-secret",
			UsePathStyle:    true,
		},
	})
	if err != nil {
		t.Fatalf("new engine: %v", err)
	}
	defer func() { _ = eng.Stop(context.Background()) }()

	if eng.Bundles == nil {
		t.Fatal("expected s3 bundle provider to be configured")
	}
}
