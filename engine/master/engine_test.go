package master

import (
	"context"
	"net"
	"path/filepath"
	"testing"

	enginepkg "github.com/jabberwocky238/luna-edge/engine"
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

func TestBuildSnapshotVersionsUseMaxOfRouteCertificateAndDNSVersions(t *testing.T) {
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
		ID:           "domain-1",
		ZoneID:       "zone-1",
		Hostname:     "app.example.com",
		StateVersion: 1,
	}); err != nil {
		t.Fatalf("upsert domain: %v", err)
	}
	if err := repo.ServiceBindings().UpsertResource(ctx, &metadata.ServiceBinding{
		ID:           "binding-1",
		DomainID:     "domain-1",
		Hostname:     "app.example.com",
		ServiceID:    "svc-1",
		Namespace:    "default",
		Name:         "svc-app",
		Address:      "10.0.0.1",
		Port:         8080,
		Protocol:     "http",
		RouteVersion: 5,
	}); err != nil {
		t.Fatalf("upsert binding: %v", err)
	}
	if err := repo.CertificateRevisions().UpsertResource(ctx, &metadata.CertificateRevision{
		ID:       "cert-1",
		DomainID: "domain-1",
		ZoneID:   "zone-1",
		Hostname: "app.example.com",
		Revision: 9,
		Status:   metadata.CertificateRevisionStatusActive,
	}); err != nil {
		t.Fatalf("upsert cert: %v", err)
	}
	if err := repo.Attachments().UpsertResource(ctx, &metadata.Attachment{
		ID:                         "attach-1",
		DomainID:                   "domain-1",
		NodeID:                     "node-1",
		DesiredRouteVersion:        5,
		DesiredCertificateRevision: 9,
		DesiredDNSVersion:          7,
	}); err != nil {
		t.Fatalf("upsert attachment: %v", err)
	}

	builder, err := enginepkg.NewRepositoryProjectionBuilder(repo)
	if err != nil {
		t.Fatalf("new builder: %v", err)
	}
	eng := &Engine{
		Factory: factory,
		Repo:    repo,
		Hub:     NewHub(),
		Builder: builder,
	}

	snapshot, err := eng.BuildSnapshot(ctx, "node-1")
	if err != nil {
		t.Fatalf("build snapshot: %v", err)
	}
	if snapshot.Versions.DesiredRouteVersion != 5 || snapshot.Versions.DesiredCertificateRevision != 9 || snapshot.Versions.DesiredDNSVersion != 7 {
		t.Fatalf("unexpected snapshot versions: %+v", snapshot.Versions)
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
