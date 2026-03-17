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

func (stubReader) GetCertificateBundle(context.Context, string, uint64) (*enginepkg.CertificateBundle, error) {
	return nil, nil
}

func (stubReader) GetDomainEntryByHostname(context.Context, string) (*metadata.DomainEntryProjection, error) {
	return nil, nil
}

func (stubReader) GetDNSRecordsByHostname(context.Context, string) ([]metadata.DNSRecord, error) {
	return nil, nil
}

func (stubReader) ListDNSRecords(context.Context) ([]metadata.DNSRecord, error) {
	return nil, nil
}

func (stubReader) GetSnapshotRecordID(context.Context) (uint64, error) {
	return 0, nil
}

type stubSubscriber struct {
	calls int
}

func (s *stubSubscriber) Subscribe(ctx context.Context, nodeID string) error {
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

func TestEngineStartAttemptsSubscribe(t *testing.T) {
	t.Parallel()

	sub := &stubSubscriber{}
	eng := &Engine{
		Config: Config{
			NodeID:            "node-1",
			SubscribeSnapshot: true,
			RetryMinBackoff:   time.Millisecond,
			RetryMaxBackoff:   time.Millisecond,
		},
		Cache:      stubReader{},
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
