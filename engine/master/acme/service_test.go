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
	s.bundles[fmt.Sprintf("%s:%d", hostname, revision)] = bundle
	return nil
}

type fakeHTTP01Store struct {
	items map[string]string
}

func (s *fakeHTTP01Store) SetHTTP01Challenge(token, keyAuthorization string) {
	if s.items == nil {
		s.items = map[string]string{}
	}
	s.items[token] = keyAuthorization
}

func (s *fakeHTTP01Store) DeleteHTTP01Challenge(token string) {
	delete(s.items, token)
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
	return fakeIssuer{t: f.t, provider: provider}, nil
}

type fakeIssuer struct {
	t        *testing.T
	provider challenge.Provider
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
	seedACMEDomain(t, repo, "domain-1", "app.example.com", metadata.BackendTypeL7HTTP)

	publisher := &fakePublisher{}
	bundles := &fakeBundleStore{}
	svc := NewService(Config{
		DefaultEmail: "ops@example.com",
		DNS01TTL:     120,
	}, repo, publisher, bundles, fakeIssuerFactory{t: t, expectedType: metadata.ChallengeTypeDNS01}, nil)

	cert, err := svc.IssueCertificate(ctx, IssueRequest{
		DomainID:      "domain-1",
		ChallengeType: metadata.ChallengeTypeDNS01,
		Provider:      metadata.ProviderLetsEncrypt,
	})
	if err != nil {
		t.Fatalf("issue certificate: %v", err)
	}
	if cert == nil || cert.Revision != 1 {
		t.Fatalf("unexpected certificate revision: %+v", cert)
	}

	records, err := repo.ListDNSRecordsByQuestion(ctx, "_acme-challenge.app.example.com.", string(metadata.DNSTypeTXT))
	if err != nil {
		t.Fatalf("list dns records by question: %v", err)
	}
	if len(records) != 0 {
		t.Fatalf("expected dns records to be cleaned, got %+v", records)
	}

	domain, err := repo.GetDomainEndpointByID(ctx, "domain-1")
	if err != nil {
		t.Fatalf("get domain: %v", err)
	}
	if domain.CertID == "" || domain.CertID != cert.ID {
		t.Fatalf("expected domain cert id to be updated, got %+v", domain)
	}

	if len(publisher.nodes) != 5 {
		t.Fatalf("expected 5 publishes, got %d", len(publisher.nodes))
	}
	if bundles.bundles["app.example.com:1"] == nil {
		t.Fatal("expected bundle to be stored")
	}
}

func TestIssueCertificateHTTP01(t *testing.T) {
	t.Parallel()

	repo := newTestRepository(t)
	ctx := context.Background()
	seedACMEDomain(t, repo, "domain-1", "app.example.com", metadata.BackendTypeL7HTTP)

	publisher := &fakePublisher{}
	http01 := &fakeHTTP01Store{}
	svc := NewService(Config{
		DefaultEmail:   "ops@example.com",
		HTTP01Priority: 999,
	}, repo, publisher, &fakeBundleStore{}, fakeIssuerFactory{t: t, expectedType: metadata.ChallengeTypeHTTP01}, http01)

	cert, err := svc.IssueCertificate(ctx, IssueRequest{
		DomainID:      "domain-1",
		ChallengeType: metadata.ChallengeTypeHTTP01,
		Provider:      metadata.ProviderLetsEncrypt,
	})
	if err != nil {
		t.Fatalf("issue certificate: %v", err)
	}

	if len(http01.items) != 0 {
		t.Fatalf("expected http01 registry to be cleaned, got %+v", http01.items)
	}

	domain, err := repo.GetDomainEndpointByID(ctx, "domain-1")
	if err != nil {
		t.Fatalf("get domain: %v", err)
	}
	if domain.CertID != cert.ID {
		t.Fatalf("expected domain cert id to be updated, got %+v", domain)
	}
	if len(publisher.nodes) != 3 {
		t.Fatalf("expected 3 publishes, got %d", len(publisher.nodes))
	}
}

func TestPresentDNS01WritesAndBroadcasts(t *testing.T) {
	t.Parallel()

	repo := newTestRepository(t)
	ctx := context.Background()
	seedACMEDomain(t, repo, "domain-1", "app.example.com", metadata.BackendTypeL7HTTP)
	domain, err := repo.GetDomainEndpointByID(ctx, "domain-1")
	if err != nil {
		t.Fatalf("get domain: %v", err)
	}

	publisher := &fakePublisher{}
	svc := NewService(Config{DNS01TTL: 90}, repo, publisher, nil, fakeIssuerFactory{t: t, expectedType: metadata.ChallengeTypeDNS01}, nil)
	provider := &masterChallengeProvider{
		service:       svc,
		domain:        domain,
		challengeType: metadata.ChallengeTypeDNS01,
	}

	if err := provider.Present("app.example.com", "token-1", "token-1.key-auth"); err != nil {
		t.Fatalf("present dns01: %v", err)
	}

	records, err := repo.ListDNSRecordsByQuestion(ctx, "_acme-challenge.app.example.com.", string(metadata.DNSTypeTXT))
	if err != nil {
		t.Fatalf("list dns records by question: %v", err)
	}
	if len(records) != 1 {
		t.Fatalf("expected 1 dns record, got %+v", records)
	}
	if len(publisher.nodes) != 1 || publisher.nodes[0] != enginepkg.POD_NAME {
		t.Fatalf("unexpected publishes: %+v", publisher.nodes)
	}
}

func TestPresentHTTP01WritesAndBroadcasts(t *testing.T) {
	t.Parallel()

	repo := newTestRepository(t)
	ctx := context.Background()
	seedACMEDomain(t, repo, "domain-1", "app.example.com", metadata.BackendTypeL7HTTP)
	domain, err := repo.GetDomainEndpointByID(ctx, "domain-1")
	if err != nil {
		t.Fatalf("get domain: %v", err)
	}

	publisher := &fakePublisher{}
	http01 := &fakeHTTP01Store{}
	svc := NewService(Config{HTTP01Priority: 999}, repo, publisher, nil, fakeIssuerFactory{t: t, expectedType: metadata.ChallengeTypeHTTP01}, http01)
	provider := &masterChallengeProvider{
		service:       svc,
		domain:        domain,
		challengeType: metadata.ChallengeTypeHTTP01,
	}

	if err := provider.Present("app.example.com", "token-1", "token-1.key-auth"); err != nil {
		t.Fatalf("present http01: %v", err)
	}

	if got := http01.items["token-1"]; got != "token-1.key-auth" {
		t.Fatalf("unexpected http01 token content: %q", got)
	}
	if len(publisher.nodes) != 0 {
		t.Fatalf("unexpected publishes: %+v", publisher.nodes)
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

func seedACMEDomain(t *testing.T, repo repository.Repository, domainID, hostname string, backendType metadata.BackendType) {
	t.Helper()
	ctx := context.Background()
	mustUpsert(t, repo.DomainEndpoints().UpsertResource(ctx, &metadata.DomainEndpoint{
		ID:          domainID,
		Hostname:    hostname,
		NeedCert:    true,
		BackendType: backendType,
	}))
}

func mustUpsert(t *testing.T, err error) {
	t.Helper()
	if err != nil {
		t.Fatal(err)
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
