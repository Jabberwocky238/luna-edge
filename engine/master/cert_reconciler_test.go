package master

import (
	"context"
	"errors"
	"path/filepath"
	"testing"
	"time"

	enginepkg "github.com/jabberwocky238/luna-edge/engine"
	"github.com/jabberwocky238/luna-edge/engine/master/acme"
	"github.com/jabberwocky238/luna-edge/repository"
	"github.com/jabberwocky238/luna-edge/repository/connection"
	"github.com/jabberwocky238/luna-edge/repository/metadata"
)

type memoryBundleProvider struct {
	bundles map[string]*enginepkg.CertificateBundle
}

func (m *memoryBundleProvider) PutCertificateBundle(_ context.Context, hostname string, revision uint64, bundle *enginepkg.CertificateBundle) error {
	if m.bundles == nil {
		m.bundles = map[string]*enginepkg.CertificateBundle{}
	}
	m.bundles[bundleKey(hostname, revision)] = bundle
	return nil
}

func (m *memoryBundleProvider) FetchCertificateBundle(_ context.Context, hostname string, revision uint64) (*enginepkg.CertificateBundle, error) {
	bundle, ok := m.bundles[bundleKey(hostname, revision)]
	if !ok {
		return nil, errors.New("bundle not found")
	}
	return bundle, nil
}

func bundleKey(hostname string, revision uint64) string {
	return hostname + "|" + time.Unix(int64(revision), 0).UTC().Format(time.RFC3339Nano)
}

type fakeCertificateIssuer struct {
	repo     repository.Repository
	bundles  *memoryBundleProvider
	requests []acme.IssueRequest
}

func (f *fakeCertificateIssuer) IssueCertificate(ctx context.Context, req acme.IssueRequest) (*metadata.CertificateRevision, error) {
	f.requests = append(f.requests, req)

	domain, err := f.repo.GetDomainEndpointByID(ctx, req.DomainID)
	if err != nil {
		return nil, err
	}
	latest, err := f.repo.GetLatestCertificateRevision(ctx, req.DomainID)
	revision := uint64(1)
	if err == nil && latest != nil {
		revision = latest.Revision + 1
	}
	cert := &metadata.CertificateRevision{
		ID:               "cert-" + time.Now().UTC().Format("150405.000000000"),
		DomainEndpointID: domain.ID,
		Hostname:         domain.Hostname,
		Revision:         revision,
		ChallengeType:    req.ChallengeType,
		Provider:         acme.ProviderLetsEncrypt,
		NotBefore:        time.Now().UTC().Add(-time.Hour),
		NotAfter:         time.Now().UTC().Add(90 * 24 * time.Hour),
	}
	if err := f.repo.CertificateRevisions().UpsertResource(ctx, cert); err != nil {
		return nil, err
	}
	if err := f.bundles.PutCertificateBundle(ctx, domain.Hostname, revision, &enginepkg.CertificateBundle{
		Hostname: domain.Hostname,
		Revision: revision,
		TLSCrt:   []byte("crt"),
		TLSKey:   []byte("key"),
	}); err != nil {
		return nil, err
	}
	domain.CertID = cert.ID
	if err := f.repo.DomainEndpoints().UpsertResource(ctx, domain); err != nil {
		return nil, err
	}
	return cert, nil
}

func TestCertReconcilerIssuesWhenNeedCertAndNoCertificate(t *testing.T) {
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
		NeedCert:    true,
		BackendType: metadata.BackendTypeL7HTTP,
	}); err != nil {
		t.Fatalf("upsert domain: %v", err)
	}

	bundles := &memoryBundleProvider{}
	issuer := &fakeCertificateIssuer{repo: repo, bundles: bundles}
	reconciler := NewCertReconciler(repo, issuer, time.Hour, 30*24*time.Hour)
	if err := reconciler.scan(ctx); err != nil {
		t.Fatalf("scan: %v", err)
	}

	assertIssuedCertificate(t, ctx, repo, bundles, "domain-1", "app.example.com", 1)
	if got := issuer.requests[0].ChallengeType; got != metadata.ChallengeTypeHTTP01 {
		t.Fatalf("expected http-01 for l7 endpoint, got %s", got)
	}
}

func TestCertReconcilerNotifyIssuesForExpiringCertificate(t *testing.T) {
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
		NeedCert:    true,
		BackendType: metadata.BackendTypeL4TLSPassthrough,
		CertID:      "cert-1",
	}); err != nil {
		t.Fatalf("upsert domain: %v", err)
	}
	if err := repo.CertificateRevisions().UpsertResource(ctx, &metadata.CertificateRevision{
		ID:               "cert-1",
		DomainEndpointID: "domain-1",
		Hostname:         "app.example.com",
		Revision:         1,
		NotAfter:         time.Now().UTC().Add(12 * time.Hour),
	}); err != nil {
		t.Fatalf("upsert cert: %v", err)
	}

	bundles := &memoryBundleProvider{}
	issuer := &fakeCertificateIssuer{repo: repo, bundles: bundles}
	reconciler := NewCertReconciler(repo, issuer, time.Hour, 24*time.Hour)
	reconciler.now = func() time.Time { return time.Now().UTC() }
	if err := reconciler.reconcileHostname(ctx, "app.example.com"); err != nil {
		t.Fatalf("reconcile hostname: %v", err)
	}

	assertIssuedCertificate(t, ctx, repo, bundles, "domain-1", "app.example.com", 2)
	if got := issuer.requests[0].ChallengeType; got != metadata.ChallengeTypeDNS01 {
		t.Fatalf("expected dns-01 for l4 endpoint, got %s", got)
	}
}

func assertIssuedCertificate(t *testing.T, ctx context.Context, repo repository.Repository, bundles *memoryBundleProvider, domainID, hostname string, expectedRevision uint64) {
	t.Helper()

	domain, err := repo.GetDomainEndpointByID(ctx, domainID)
	if err != nil {
		t.Fatalf("get domain: %v", err)
	}
	if domain.CertID == "" {
		t.Fatal("expected cert id to be set")
	}

	cert := &metadata.CertificateRevision{}
	if err := repo.CertificateRevisions().GetResourceByField(ctx, cert, "id", domain.CertID); err != nil {
		t.Fatalf("get cert revision: %v", err)
	}
	if cert.Revision != expectedRevision {
		t.Fatalf("expected revision %d, got %+v", expectedRevision, cert)
	}

	bundle, err := bundles.FetchCertificateBundle(ctx, hostname, cert.Revision)
	if err != nil {
		t.Fatalf("fetch certificate bundle: %v", err)
	}
	if len(bundle.TLSCrt) == 0 || len(bundle.TLSKey) == 0 {
		t.Fatalf("expected certificate bytes, got %+v", bundle)
	}
}
