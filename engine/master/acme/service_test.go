package acme

import (
	"context"
	"crypto/rand"
	"crypto/rsa"
	"crypto/x509"
	"crypto/x509/pkix"
	"encoding/pem"
	"fmt"
	"math/big"
	"path/filepath"
	"testing"
	"time"

	"github.com/go-acme/lego/v4/certificate"
	"github.com/go-acme/lego/v4/challenge"
	enginepkg "github.com/jabberwocky238/luna-edge/engine"
	"github.com/jabberwocky238/luna-edge/repository"
	"github.com/jabberwocky238/luna-edge/repository/connection"
	"github.com/jabberwocky238/luna-edge/repository/metadata"
)

type fakePublisher struct {
	nodes []string
}

func (p *fakePublisher) PublishNode(_ context.Context, nodeID string) error {
	p.nodes = append(p.nodes, nodeID)
	return nil
}

type fakeBundleStore struct {
	bundles map[string]*enginepkg.CertificateBundle
}

func (s *fakeBundleStore) PutCertificateBundle(_ context.Context, hostname string, revision uint64, bundle *enginepkg.CertificateBundle) error {
	if s.bundles == nil {
		s.bundles = map[string]*enginepkg.CertificateBundle{}
	}
	s.bundles[hostname] = bundle
	return nil
}

type fakeIssuerFactory struct {
	t            *testing.T
	expectedType metadata.ChallengeType
}

func (f fakeIssuerFactory) New(_ IssuerConfig, challengeType metadata.ChallengeType, provider challenge.Provider) (CertificateIssuer, error) {
	f.t.Helper()
	if challengeType != f.expectedType {
		f.t.Fatalf("unexpected challenge type: %s", challengeType)
	}
	return fakeIssuer{t: f.t, provider: provider, challengeType: challengeType}, nil
}

type fakeIssuer struct {
	t             *testing.T
	provider      challenge.Provider
	challengeType metadata.ChallengeType
}

func (i fakeIssuer) Obtain(_ context.Context, domains []string) (*certificate.Resource, error) {
	i.t.Helper()
	token := "token-1"
	keyAuth := "token-1.key-auth"
	if err := i.provider.Present(domains[0], token, keyAuth); err != nil {
		return nil, err
	}
	if err := i.provider.CleanUp(domains[0], token, keyAuth); err != nil {
		return nil, err
	}
	return newCertificateResource(i.t, domains[0]), nil
}

func TestIssueCertificateDNS01(t *testing.T) {
	t.Parallel()

	repo := newTestRepository(t)
	ctx := context.Background()
	seedACMEDomain(t, repo, "domain-1", "app.example.com")

	publisher := &fakePublisher{}
	bundles := &fakeBundleStore{}
	svc := NewService(Config{
		DefaultEmail: "ops@example.com",
		DNS01TTL:     120,
	}, repo, publisher, bundles)
	svc.issuers = fakeIssuerFactory{t: t, expectedType: metadata.ChallengeTypeDNS01}
	svc.now = fixedClock()
	svc.idSuffix = sequentialIDs()

	cert, err := svc.IssueCertificate(ctx, IssueRequest{
		DomainID:      "domain-1",
		ChallengeType: metadata.ChallengeTypeDNS01,
		Provider:      ProviderLetsEncrypt,
	})
	if err != nil {
		t.Fatalf("issue certificate: %v", err)
	}
	if cert == nil || cert.Revision != 1 {
		t.Fatalf("unexpected certificate revision: %+v", cert)
	}

	challenges, err := repo.ListACMEChallengesByOrderID(ctx, "acmeorder-id-002")
	if err != nil {
		t.Fatalf("list challenges: %v", err)
	}
	if len(challenges) != 1 || challenges[0].Status != metadata.ACMEChallengeStatusCleaned {
		t.Fatalf("unexpected challenges: %+v", challenges)
	}

	records, err := repo.ListDNSRecordsByDomainID(ctx, "domain-1")
	if err != nil {
		t.Fatalf("list dns records: %v", err)
	}
	if len(records) != 0 {
		t.Fatalf("expected dns records to be cleaned, got %+v", records)
	}

	status, err := repo.GetDomainEndpointStatus(ctx, "domain-1")
	if err != nil {
		t.Fatalf("get status: %v", err)
	}
	if !status.CertificateReady || status.CertificateRevision != 1 || status.Phase != metadata.DomainPhaseReady {
		t.Fatalf("unexpected status: %+v", status)
	}
	if len(publisher.nodes) != 4 {
		t.Fatalf("expected 4 publishes, got %d", len(publisher.nodes))
	}
	if bundles.bundles["app.example.com"] == nil {
		t.Fatal("expected bundle to be stored")
	}
}

func TestIssueCertificateHTTP01(t *testing.T) {
	t.Parallel()

	repo := newTestRepository(t)
	ctx := context.Background()
	seedACMEDomain(t, repo, "domain-1", "app.example.com")

	publisher := &fakePublisher{}
	svc := NewService(Config{
		DefaultEmail:   "ops@example.com",
		HTTP01Priority: 999,
	}, repo, publisher, &fakeBundleStore{})
	svc.issuers = fakeIssuerFactory{t: t, expectedType: metadata.ChallengeTypeHTTP01}
	svc.now = fixedClock()
	svc.idSuffix = sequentialIDs()

	if _, err := svc.IssueCertificate(ctx, IssueRequest{
		DomainID:      "domain-1",
		ChallengeType: metadata.ChallengeTypeHTTP01,
		Provider:      ProviderLetsEncrypt,
	}); err != nil {
		t.Fatalf("issue certificate: %v", err)
	}

	routes, err := repo.ListHTTPRoutesByDomainID(ctx, "domain-1")
	if err != nil {
		t.Fatalf("list http routes: %v", err)
	}
	if len(routes) != 0 {
		t.Fatalf("expected http01 routes to be cleaned, got %+v", routes)
	}

	bindings, err := repo.ListServiceBindingsByDomainID(ctx, "domain-1")
	if err != nil {
		t.Fatalf("list bindings: %v", err)
	}
	if len(bindings) != 0 {
		t.Fatalf("expected http01 binding cleanup, got %+v", bindings)
	}

	var certs []metadata.CertificateRevision
	if err := repo.CertificateRevisions().ListResource(ctx, &certs, "revision asc"); err != nil {
		t.Fatalf("list cert revisions: %v", err)
	}
	if len(certs) != 1 || certs[0].Status != metadata.CertificateRevisionStatusActive {
		t.Fatalf("unexpected cert revisions: %+v", certs)
	}
}

func newTestRepository(t *testing.T) repository.Repository {
	t.Helper()
	factory, err := repository.NewFactory(connection.Config{
		Driver:      connection.DriverSQLite,
		Path:        filepath.Join(t.TempDir(), "meta.db"),
		AutoMigrate: true,
	})
	if err != nil {
		t.Fatalf("new repository factory: %v", err)
	}
	t.Cleanup(func() { _ = factory.Close() })
	return factory.Repository()
}

func seedACMEDomain(t *testing.T, repo repository.Repository, domainID, hostname string) {
	t.Helper()
	ctx := context.Background()
	mustUpsert(t, repo.Zones().UpsertResource(ctx, &metadata.Zone{
		ID:                  "zone-1",
		Name:                "example.com",
		DefaultACMEProvider: ProviderLetsEncrypt,
	}))
	mustUpsert(t, repo.DomainEndpoints().UpsertResource(ctx, &metadata.DomainEndpoint{
		ID:           domainID,
		ZoneID:       "zone-1",
		Hostname:     hostname,
		BackendType:  "l7",
		Generation:   1,
		StateVersion: 1,
	}))
	mustUpsert(t, repo.Attachments().UpsertResource(ctx, &metadata.Attachment{
		ID:                         "attach-1",
		DomainID:                   domainID,
		NodeID:                     "node-1",
		Listener:                   "edge-http",
		DesiredRouteVersion:        1,
		DesiredDNSVersion:          1,
		DesiredCertificateRevision: 0,
		State:                      metadata.AttachmentStateReady,
	}))
}

func mustUpsert(t *testing.T, err error) {
	t.Helper()
	if err != nil {
		t.Fatal(err)
	}
}

func fixedClock() func() time.Time {
	now := time.Date(2026, 3, 16, 12, 0, 0, 0, time.UTC)
	return func() time.Time { return now }
}

func sequentialIDs() func() string {
	var seq int
	return func() string {
		seq++
		return fmt.Sprintf("id-%03d", seq)
	}
}

func newCertificateResource(t *testing.T, hostname string) *certificate.Resource {
	t.Helper()
	priv, err := rsa.GenerateKey(rand.Reader, 2048)
	if err != nil {
		t.Fatalf("generate key: %v", err)
	}
	template := &x509.Certificate{
		SerialNumber: big.NewInt(1),
		Subject:      pkix.Name{CommonName: hostname},
		NotBefore:    time.Now().Add(-time.Hour),
		NotAfter:     time.Now().Add(24 * time.Hour),
		KeyUsage:     x509.KeyUsageDigitalSignature | x509.KeyUsageKeyEncipherment,
		ExtKeyUsage:  []x509.ExtKeyUsage{x509.ExtKeyUsageServerAuth},
		DNSNames:     []string{hostname},
	}
	der, err := x509.CreateCertificate(rand.Reader, template, template, &priv.PublicKey, priv)
	if err != nil {
		t.Fatalf("create cert: %v", err)
	}
	certPEM := pem.EncodeToMemory(&pem.Block{Type: "CERTIFICATE", Bytes: der})
	keyPEM := pem.EncodeToMemory(&pem.Block{Type: "RSA PRIVATE KEY", Bytes: x509.MarshalPKCS1PrivateKey(priv)})
	return &certificate.Resource{
		Domain:      hostname,
		Certificate: certPEM,
		PrivateKey:  keyPEM,
	}
}
