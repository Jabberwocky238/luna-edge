package slave

import (
	"context"
	"os"
	"path/filepath"
	"testing"

	enginepkg "github.com/jabberwocky238/luna-edge/engine"
	"github.com/jabberwocky238/luna-edge/repository/metadata"
)

type stubBundleFetcher struct {
	bundles map[string]*enginepkg.CertificateBundle
}

func (s stubBundleFetcher) FetchCertificateBundle(_ context.Context, hostname string, revision uint64) (*enginepkg.CertificateBundle, error) {
	if bundle, ok := s.bundles[hostname]; ok && bundle.Revision == revision {
		return bundle, nil
	}
	return nil, nil
}

func TestLocalStoreSyncChangelogCertificatesWritesWithoutPruningOtherHosts(t *testing.T) {
	t.Parallel()

	store, err := NewLocalStore(t.TempDir())
	if err != nil {
		t.Fatalf("new local store: %v", err)
	}
	defer func() { _ = store.Close() }()

	store.SetCertificateBundleFetcher(stubBundleFetcher{
		bundles: map[string]*enginepkg.CertificateBundle{
			"app.example.com": {
				Hostname:     "app.example.com",
				Revision:     3,
				TLSCrt:       []byte("crt"),
				TLSKey:       []byte("key"),
				MetadataJSON: []byte(`{"hostname":"app.example.com","revision":3}`),
			},
		},
	})

	staleDir := filepath.Join(store.CertificatesRoot(), "old.example.com")
	if err := os.MkdirAll(staleDir, 0o755); err != nil {
		t.Fatalf("mkdir stale dir: %v", err)
	}

	changelog := &enginepkg.ChangeNotification{
		SnapshotRecordID: 10,
		DomainEntry: &metadata.DomainEntryProjection{
			ID:       "domain-1",
			Hostname: "app.example.com",
			Cert: &metadata.CertificateRevision{
				ID:       "cert-1",
				Revision: 3,
			},
		},
	}

	if err := store.SyncChangelogCertificates(context.Background(), changelog); err != nil {
		t.Fatalf("sync changelog certificates: %v", err)
	}

	if _, err := os.Stat(filepath.Join(store.CertificatesRoot(), "app.example.com", "tls.crt")); err != nil {
		t.Fatalf("expected cert file: %v", err)
	}
	if _, err := os.Stat(filepath.Join(store.CertificatesRoot(), "app.example.com", "tls.key")); err != nil {
		t.Fatalf("expected key file: %v", err)
	}
	bundle, err := store.GetCertificateBundle(context.Background(), "app.example.com", 0)
	if err != nil {
		t.Fatalf("get certificate bundle: %v", err)
	}
	if bundle.Hostname != "app.example.com" || bundle.Revision != 3 {
		t.Fatalf("unexpected certificate bundle: %+v", bundle)
	}
	if _, err := os.Stat(staleDir); err != nil {
		t.Fatalf("expected unrelated cert dir to remain, got err=%v", err)
	}
}

func TestLocalStoreSyncChangelogCertificatesDeletesRemovedHost(t *testing.T) {
	t.Parallel()

	store, err := NewLocalStore(t.TempDir())
	if err != nil {
		t.Fatalf("new local store: %v", err)
	}
	defer func() { _ = store.Close() }()

	dir := filepath.Join(store.CertificatesRoot(), "app.example.com")
	if err := os.MkdirAll(dir, 0o755); err != nil {
		t.Fatalf("mkdir cert dir: %v", err)
	}
	if err := os.WriteFile(filepath.Join(dir, "tls.crt"), []byte("crt"), 0o644); err != nil {
		t.Fatalf("write crt: %v", err)
	}

	changelog := &enginepkg.ChangeNotification{
		SnapshotRecordID: 11,
		DomainEntry: &metadata.DomainEntryProjection{
			ID:       "domain-1",
			Hostname: "app.example.com",
			Deleted:  true,
		},
	}

	if err := store.SyncChangelogCertificates(context.Background(), changelog); err != nil {
		t.Fatalf("sync changelog certificates: %v", err)
	}
	if _, err := os.Stat(dir); !os.IsNotExist(err) {
		t.Fatalf("expected cert dir to be removed, got err=%v", err)
	}
}
