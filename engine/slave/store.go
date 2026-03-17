package slave

import (
	"context"
	"fmt"
	"os"
	"path/filepath"
	"strings"

	"github.com/jabberwocky238/luna-edge/engine"
	"gorm.io/driver/sqlite"
	"gorm.io/gorm"
)

const (
	DefaultMetadataDBName  = "meta.db"
	DefaultCertificatesDir = "certs"
	snapshotCursorStateKey = "snapshot_record_id"
)

func normalizeCacheRoot(root string) (string, error) {
	root = strings.TrimSpace(root)
	if root == "" {
		return "", fmt.Errorf("cache root is required")
	}
	return root, nil
}

func metadataDBPathFor(cacheRoot string) string {
	return filepath.Join(cacheRoot, DefaultMetadataDBName)
}

func certificatesRootFor(cacheRoot string) string {
	return filepath.Join(cacheRoot, DefaultCertificatesDir)
}

type LocalStore struct {
	cacheRoot string
	certRoot  string
	fetcher   CertificateBundleFetcher
	db        *gorm.DB
}

type CertificateBundleFetcher interface {
	FetchCertificateBundle(ctx context.Context, hostname string, revision uint64) (*engine.CertificateBundle, error)
}

func (s *LocalStore) MetadataDBPath() string {
	return metadataDBPathFor(s.cacheRoot)
}

func (s *LocalStore) CertificatesRoot() string {
	return certificatesRootFor(s.cacheRoot)
}

func NewLocalStore(cacheRoot string) (*LocalStore, error) {
	store := new(LocalStore)
	normalized, err := normalizeCacheRoot(cacheRoot)
	if err != nil {
		return nil, err
	}
	store.cacheRoot = normalized
	if err := os.MkdirAll(store.cacheRoot, 0o755); err != nil {
		return nil, err
	}
	store.certRoot = store.CertificatesRoot()
	if err := os.MkdirAll(store.certRoot, 0o755); err != nil {
		return nil, err
	}
	db, err := gorm.Open(sqlite.Open(store.MetadataDBPath()), &gorm.Config{})
	if err != nil {
		return nil, err
	}
	store.db = db
	if err := store.initSchema(); err != nil {
		return nil, err
	}
	return store, nil
}

func (s *LocalStore) Close() error {
	if s == nil || s.db == nil {
		return nil
	}
	sqlDB, err := s.db.DB()
	if err != nil {
		return err
	}
	return sqlDB.Close()
}

func (s *LocalStore) SetCertificateBundleFetcher(fetcher CertificateBundleFetcher) {
	if s == nil {
		return
	}
	s.fetcher = fetcher
}
