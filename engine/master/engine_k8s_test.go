package master

import (
	"context"
	"path/filepath"
	"testing"
	"time"

	"github.com/jabberwocky238/luna-edge/repository"
	"github.com/jabberwocky238/luna-edge/repository/connection"
	"github.com/jabberwocky238/luna-edge/repository/metadata"
)

func TestEngineAutoCertForIngressWebsecure(t *testing.T) {
	t.Parallel()
	testEngineAutoCertForL7HTTPS(t, "ingress-websecure.example.com")
}

func TestEngineAutoCertForGatewayHTTPS(t *testing.T) {
	t.Parallel()
	testEngineAutoCertForL7HTTPS(t, "gateway-https.example.com")
}

func testEngineAutoCertForL7HTTPS(t *testing.T, hostname string) {
	t.Helper()

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
	domainID := "domain-" + hostname
	if err := repo.DomainEndpoints().UpsertResource(ctx, &metadata.DomainEndpoint{
		ID:          domainID,
		Hostname:    hostname,
		NeedCert:    true,
		BackendType: metadata.BackendTypeL7HTTPS,
	}); err != nil {
		t.Fatalf("upsert domain: %v", err)
	}

	bundles := &memoryBundleProvider{}
	issuer := &fakeCertificateIssuer{repo: repo, bundles: bundles}

	eng := &Engine{
		Repo:    repo,
		Bundles: bundles,
		Certs:   NewCertReconciler(repo, issuer, time.Hour, 30*24*time.Hour),
	}
	eng.Certs.Start()
	defer eng.Certs.Stop()

	eng.Notify(hostname)

	assertEventuallyIssuedCertificate(t, ctx, repo, bundles, domainID, hostname, 1)
}

func assertEventuallyIssuedCertificate(t *testing.T, ctx context.Context, repo repository.Repository, bundles *memoryBundleProvider, domainID, hostname string, expectedRevision uint64) {
	t.Helper()

	deadline := time.Now().Add(3 * time.Second)
	for time.Now().Before(deadline) {
		domain, err := repo.GetDomainEndpointByID(ctx, domainID)
		if err == nil && domain != nil && domain.CertID != "" {
			cert := &metadata.CertificateRevision{}
			if err := repo.CertificateRevisions().GetResourceByField(ctx, cert, "id", domain.CertID); err == nil && cert.Revision == expectedRevision {
				if bundle, bundleErr := bundles.FetchCertificateBundle(ctx, hostname, cert.Revision); bundleErr == nil && len(bundle.TLSCrt) > 0 && len(bundle.TLSKey) > 0 {
					return
				}
			}
		}
		time.Sleep(20 * time.Millisecond)
	}

	domain, err := repo.GetDomainEndpointByID(ctx, domainID)
	if err != nil {
		t.Fatalf("get domain after notify: %v", err)
	}
	t.Fatalf("expected certificate issuance for %s, got domain %+v", hostname, domain)
}
