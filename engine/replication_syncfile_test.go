package engine_test

import (
	"context"
	"crypto/rand"
	"crypto/rsa"
	"crypto/x509"
	"crypto/x509/pkix"
	"encoding/json"
	"encoding/pem"
	"fmt"
	"math/big"
	"net"
	"os"
	"path/filepath"
	"testing"
	"time"

	enginepkg "github.com/jabberwocky238/luna-edge/engine"
	masterpkg "github.com/jabberwocky238/luna-edge/engine/master"
	slavepkg "github.com/jabberwocky238/luna-edge/engine/slave"
	"github.com/jabberwocky238/luna-edge/ingress"
	"github.com/jabberwocky238/luna-edge/replication/replpb"
	"github.com/jabberwocky238/luna-edge/repository"
	"github.com/jabberwocky238/luna-edge/repository/connection"
	"github.com/jabberwocky238/luna-edge/repository/metadata"
	"google.golang.org/grpc"
	"google.golang.org/grpc/codes"
	"google.golang.org/grpc/status"
)

type bundleProvider struct {
	bundles map[string]*enginepkg.CertificateBundle
}

func (p bundleProvider) FetchCertificateBundle(_ context.Context, hostname string, revision uint64) (*enginepkg.CertificateBundle, error) {
	return p.bundles[certificateBundleKey(hostname, revision)], nil
}

func TestReplicationSlavePullsCertificateFilesFromMaster(t *testing.T) {
	t.Parallel()

	bundle := newCertificateBundle(t, "app.example.com", 2)
	masterFactory, err := repository.NewFactory(connection.Config{
		Driver:      connection.DriverSQLite,
		Path:        filepath.Join(t.TempDir(), "master.db"),
		AutoMigrate: true,
	})
	if err != nil {
		t.Fatalf("new master factory: %v", err)
	}
	defer func() { _ = masterFactory.Close() }()

	masterRepo := masterFactory.Repository()
	seedMasterProjectionWithCertificate(t, masterRepo, bundle)

	builder, err := enginepkg.NewRepositoryProjectionBuilder(masterRepo)
	if err != nil {
		t.Fatalf("new builder: %v", err)
	}
	masterEngine := &masterpkg.Engine{
		Factory: masterFactory,
		Repo:    masterRepo,
		Hub:     masterpkg.NewHub(),
		Builder: builder,
		Bundles: bundleProvider{
			bundles: map[string]*enginepkg.CertificateBundle{
				certificateBundleKey(bundle.Hostname, bundle.Revision): bundle,
			},
		},
	}

	lis, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatalf("listen: %v", err)
	}
	defer func() { _ = lis.Close() }()

	grpcServer := grpc.NewServer()
	defer grpcServer.Stop()
	replpb.RegisterReplicationServiceServer(grpcServer, masterEngine)
	go func() {
		_ = grpcServer.Serve(lis)
	}()

	cacheRoot := t.TempDir()
	slaveStore, err := slavepkg.NewLocalStore(cacheRoot)
	if err != nil {
		t.Fatalf("new slave store: %v", err)
	}
	defer func() { _ = slaveStore.Close() }()

	slaveEngine, err := slavepkg.New(slavepkg.Config{
		NodeID:            "node-1",
		MasterAddress:     lis.Addr().String(),
		SubscribeSnapshot: true,
		RetryMinBackoff:   10 * time.Millisecond,
		RetryMaxBackoff:   20 * time.Millisecond,
	}, cacheRoot, slaveStore, slaveStore)
	if err != nil {
		t.Fatalf("new slave engine: %v", err)
	}

	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	errCh := make(chan error, 1)
	go func() {
		errCh <- slaveEngine.Start(ctx)
	}()

	certRoot := slaveStore.CertificatesRoot()
	waitForCondition(t, func() bool {
		_, err := os.Stat(filepath.Join(certRoot, "app.example.com", "tls.crt"))
		return err == nil
	})
	waitForCondition(t, func() bool {
		_, err := os.Stat(filepath.Join(certRoot, "app.example.com", "tls.key"))
		return err == nil
	})
	waitForCondition(t, func() bool {
		_, err := os.Stat(filepath.Join(certRoot, "app.example.com", "metadata.json"))
		return err == nil
	})

	resolver, err := ingress.NewLunaTLSCertResolver(certRoot, 8)
	if err != nil {
		t.Fatalf("new resolver: %v", err)
	}
	cert, err := resolver.Load("app.example.com")
	if err != nil {
		t.Fatalf("load synced certificate: %v", err)
	}
	if cert == nil || len(cert.Certificate) == 0 {
		t.Fatal("expected synced certificate bytes")
	}

	cancel()
	select {
	case err := <-errCh:
		if err != nil && err != context.Canceled && status.Code(err) != codes.Canceled {
			t.Fatalf("unexpected slave start error: %v", err)
		}
	case <-time.After(2 * time.Second):
		t.Fatal("timed out waiting for slave to stop")
	}
}

func seedMasterProjectionWithCertificate(t *testing.T, repo repository.Repository, bundle *enginepkg.CertificateBundle) {
	t.Helper()
	ctx := context.Background()
	mustUpsert(t, repo.DomainEndpoints().UpsertResource(ctx, &metadata.DomainEndpoint{
		ID:           "domain-1",
		ZoneID:       "zone-1",
		Hostname:     bundle.Hostname,
		Generation:   1,
		StateVersion: 7,
	}))
	mustUpsert(t, repo.ServiceBindings().UpsertResource(ctx, &metadata.ServiceBinding{
		ID:           "binding-1",
		DomainID:     "domain-1",
		Hostname:     bundle.Hostname,
		ServiceID:    "svc-1",
		Namespace:    "default",
		Name:         "svc-app",
		Address:      "10.0.0.1",
		Port:         8080,
		Protocol:     "http",
		RouteVersion: 1,
		BackendJSON:  `{"kind":"service"}`,
	}))
	mustUpsert(t, repo.CertificateRevisions().UpsertResource(ctx, &metadata.CertificateRevision{
		ID:             "cert-1",
		DomainID:       "domain-1",
		ZoneID:         "zone-1",
		Hostname:       bundle.Hostname,
		Revision:       bundle.Revision,
		Status:         metadata.CertificateRevisionStatusActive,
		ArtifactBucket: "master",
		ArtifactPrefix: "bundle/app.example.com",
	}))
	mustUpsert(t, repo.DomainEndpointStatuses().UpsertResource(ctx, &metadata.DomainEndpointStatus{
		DomainEndpointID:    "domain-1",
		CertificateRevision: bundle.Revision,
		CertificateReady:    true,
		Ready:               true,
		Phase:               metadata.DomainPhaseReady,
	}))
	mustUpsert(t, repo.Attachments().UpsertResource(ctx, &metadata.Attachment{
		ID:                         "attach-1",
		DomainID:                   "domain-1",
		NodeID:                     "node-1",
		Listener:                   "edge-http",
		DesiredRouteVersion:        7,
		DesiredDNSVersion:          7,
		DesiredCertificateRevision: bundle.Revision,
		State:                      metadata.AttachmentStateReady,
	}))
}

func newCertificateBundle(t *testing.T, hostname string, revision uint64) *enginepkg.CertificateBundle {
	t.Helper()

	priv, err := rsa.GenerateKey(rand.Reader, 2048)
	if err != nil {
		t.Fatalf("generate key: %v", err)
	}
	template := &x509.Certificate{
		SerialNumber: big.NewInt(1),
		Subject:      pkix.Name{CommonName: hostname},
		NotBefore:    time.Now().Add(-time.Hour),
		NotAfter:     time.Now().Add(time.Hour),
		KeyUsage:     x509.KeyUsageDigitalSignature | x509.KeyUsageKeyEncipherment,
		ExtKeyUsage:  []x509.ExtKeyUsage{x509.ExtKeyUsageServerAuth},
		DNSNames:     []string{hostname},
	}
	der, err := x509.CreateCertificate(rand.Reader, template, template, &priv.PublicKey, priv)
	if err != nil {
		t.Fatalf("create certificate: %v", err)
	}
	metadataJSON, err := json.Marshal(map[string]any{
		"hostname": hostname,
		"revision": revision,
	})
	if err != nil {
		t.Fatalf("marshal metadata: %v", err)
	}
	return &enginepkg.CertificateBundle{
		Hostname:     hostname,
		Revision:     revision,
		TLSCrt:       pem.EncodeToMemory(&pem.Block{Type: "CERTIFICATE", Bytes: der}),
		TLSKey:       pem.EncodeToMemory(&pem.Block{Type: "RSA PRIVATE KEY", Bytes: x509.MarshalPKCS1PrivateKey(priv)}),
		MetadataJSON: metadataJSON,
	}
}

func certificateBundleKey(hostname string, revision uint64) string {
	return fmt.Sprintf("%s:%d", hostname, revision)
}
