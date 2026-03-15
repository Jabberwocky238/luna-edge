package slave

import (
	"context"
	"crypto/rand"
	"crypto/rsa"
	"crypto/x509"
	"crypto/x509/pkix"
	"encoding/pem"
	"math/big"
	"os"
	"path/filepath"
	"testing"
	"time"

	enginepkg "github.com/jabberwocky238/luna-edge/engine"
	"github.com/jabberwocky238/luna-edge/ingress"
	"github.com/jabberwocky238/luna-edge/repository/metadata"
)

type stubReader struct{}

func (stubReader) GetRouteByHostname(context.Context, string) (*enginepkg.RouteRecord, error) {
	return nil, nil
}

func (stubReader) GetBindingByHostname(context.Context, string) (*enginepkg.BindingRecord, error) {
	return &enginepkg.BindingRecord{Hostname: "example.com", Address: "127.0.0.1", Port: 8080, Protocol: "http"}, nil
}

func (stubReader) GetCertificate(context.Context, string, uint64) (*enginepkg.CertificateRecord, error) {
	return nil, nil
}

func (stubReader) ListAssignments(context.Context, string) ([]enginepkg.AssignmentRecord, error) {
	return nil, nil
}

func (stubReader) GetVersions(context.Context, string) (enginepkg.VersionVector, error) {
	return enginepkg.VersionVector{}, nil
}

type stubVersionReader struct {
	versions enginepkg.VersionVector
}

func (r stubVersionReader) GetRouteByHostname(context.Context, string) (*enginepkg.RouteRecord, error) {
	return nil, nil
}

func (r stubVersionReader) GetBindingByHostname(context.Context, string) (*enginepkg.BindingRecord, error) {
	return nil, nil
}

func (r stubVersionReader) GetCertificate(context.Context, string, uint64) (*enginepkg.CertificateRecord, error) {
	return nil, nil
}

func (r stubVersionReader) ListAssignments(context.Context, string) ([]enginepkg.AssignmentRecord, error) {
	return nil, nil
}

func (r stubVersionReader) GetVersions(context.Context, string) (enginepkg.VersionVector, error) {
	return r.versions, nil
}

type stubSubscriber struct {
	known enginepkg.VersionVector
	calls int
}

func (s *stubSubscriber) Subscribe(ctx context.Context, nodeID string, known enginepkg.VersionVector) error {
	s.known = known
	s.calls++
	return context.Canceled
}

func TestEngineReadCacheExposesUnderlyingReader(t *testing.T) {
	t.Parallel()

	engine := &Engine{
		Cache: stubReader{},
	}
	if _, ok := engine.ReadCache().(stubReader); !ok {
		t.Fatalf("engine did not return underlying cache reader")
	}
}

func TestIngressResolverLoadsDomainCertFromRoot(t *testing.T) {
	t.Parallel()

	root := t.TempDir()
	writeDomainCertPair(t, root, "example.com")

	resolver, err := ingress.NewLunaTLSCertResolver(root, 8)
	if err != nil {
		t.Fatalf("create resolver: %v", err)
	}
	cert, err := resolver.Load("example.com")
	if err != nil {
		t.Fatalf("load cert from root: %v", err)
	}
	if cert == nil || len(cert.Certificate) == 0 {
		t.Fatalf("expected resolver to load certificate")
	}
}

func TestLocalStoreKeepsOnlyLatestCertificateRevision(t *testing.T) {
	t.Parallel()

	store, err := NewLocalStore(t.TempDir())
	if err != nil {
		t.Fatalf("new local store: %v", err)
	}
	defer func() { _ = store.Close() }()

	ctx := context.Background()
	first := &enginepkg.Snapshot{
		Certificates: []enginepkg.CertificateRecord{{
			ID:       "cert-1",
			DomainID: "domain-1",
			Hostname: "example.com",
			Revision: 1,
			Status:   "ready",
		}},
	}
	second := &enginepkg.Snapshot{
		Certificates: []enginepkg.CertificateRecord{{
			ID:       "cert-2",
			DomainID: "domain-1",
			Hostname: "example.com",
			Revision: 2,
			Status:   "ready",
		}},
	}
	if err := store.ApplySnapshot(ctx, first); err != nil {
		t.Fatalf("apply first cert event: %v", err)
	}
	if err := store.ApplySnapshot(ctx, second); err != nil {
		t.Fatalf("apply second cert event: %v", err)
	}

	var count int64
	if err := store.db.WithContext(ctx).Model(&struct{ metadata.CertificateRevision }{}).Where("hostname = ?", "example.com").Count(&count).Error; err != nil {
		t.Fatalf("count cert revisions: %v", err)
	}
	if count != 1 {
		t.Fatalf("expected only one local certificate revision, got %d", count)
	}

	cert, err := store.GetCertificate(ctx, "example.com", 0)
	if err != nil {
		t.Fatalf("get current certificate: %v", err)
	}
	if cert.Revision != 2 {
		t.Fatalf("expected latest revision 2, got %d", cert.Revision)
	}
	if _, err := store.GetCertificate(ctx, "example.com", 1); err == nil {
		t.Fatalf("expected old revision lookup to fail")
	}
}

func TestEngineStartRestoresSubscriptionVersionsFromCache(t *testing.T) {
	t.Parallel()

	sub := &stubSubscriber{}
	eng := &Engine{
		Config: Config{
			NodeID:            "node-1",
			SubscribeSnapshot: true,
			RetryMinBackoff:   time.Millisecond,
			RetryMaxBackoff:   time.Millisecond,
		},
		Cache: stubVersionReader{versions: enginepkg.VersionVector{
			DesiredRouteVersion:        42,
			DesiredCertificateRevision: 9,
			DesiredDNSVersion:          7,
		}},
		Subscriber: sub,
	}

	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	err := eng.Start(ctx)
	if err != context.Canceled {
		t.Fatalf("expected canceled context, got %v", err)
	}
	if sub.calls != 1 {
		t.Fatalf("expected one subscribe attempt, got %d", sub.calls)
	}
	if sub.known.DesiredRouteVersion != 42 || sub.known.DesiredCertificateRevision != 9 || sub.known.DesiredDNSVersion != 7 {
		t.Fatalf("unexpected known versions: %+v", sub.known)
	}
}

func writeDomainCertPair(t *testing.T, certRoot, hostname string) {
	t.Helper()

	priv, err := rsa.GenerateKey(rand.Reader, 2048)
	if err != nil {
		t.Fatalf("generate key: %v", err)
	}
	template := &x509.Certificate{
		SerialNumber: big.NewInt(1),
		Subject: pkix.Name{
			CommonName: hostname,
		},
		NotBefore:             time.Now().Add(-time.Hour),
		NotAfter:              time.Now().Add(time.Hour),
		KeyUsage:              x509.KeyUsageDigitalSignature | x509.KeyUsageKeyEncipherment,
		ExtKeyUsage:           []x509.ExtKeyUsage{x509.ExtKeyUsageServerAuth},
		BasicConstraintsValid: true,
		DNSNames:              []string{hostname},
	}
	der, err := x509.CreateCertificate(rand.Reader, template, template, &priv.PublicKey, priv)
	if err != nil {
		t.Fatalf("create certificate: %v", err)
	}

	dir := filepath.Join(certRoot, hostname)
	if err := os.MkdirAll(dir, 0o755); err != nil {
		t.Fatalf("mkdir cert dir: %v", err)
	}
	crtPath := filepath.Join(dir, "tls.crt")
	keyPath := filepath.Join(dir, "tls.key")
	if err := os.WriteFile(crtPath, pem.EncodeToMemory(&pem.Block{Type: "CERTIFICATE", Bytes: der}), 0o644); err != nil {
		t.Fatalf("write cert: %v", err)
	}
	if err := os.WriteFile(keyPath, pem.EncodeToMemory(&pem.Block{Type: "RSA PRIVATE KEY", Bytes: x509.MarshalPKCS1PrivateKey(priv)}), 0o600); err != nil {
		t.Fatalf("write key: %v", err)
	}
}
