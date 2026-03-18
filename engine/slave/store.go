package slave

import (
	"context"
	"fmt"
	"os"
	"path/filepath"
	"strings"

	"github.com/jabberwocky238/luna-edge/engine"
	"github.com/jabberwocky238/luna-edge/repository/metadata"
	"gorm.io/driver/sqlite"
	"gorm.io/gorm"
)

const (
	DefaultMetadataDBName  = "meta.db"
	DefaultCertificatesDir = "certs"
)

type BundleFetcher interface {
	FetchCertificateBundle(ctx context.Context, hostname string, revision uint64) (*engine.CertificateBundle, error)
}

type LocalStore struct {
	CacheRoot     string
	certRoot      string
	db            *gorm.DB
	bundleFetcher BundleFetcher
	dnsChan       chan []metadata.DNSRecord
}

func (s *LocalStore) MetadataDBPath() string {
	return filepath.Join(s.CacheRoot, DefaultMetadataDBName)
}

func (s *LocalStore) CertificatesRoot() string {
	return filepath.Join(s.CacheRoot, DefaultCertificatesDir)
}

func NewLocalStore(cacheRoot string, bundleFetcher BundleFetcher, dnsChan chan []metadata.DNSRecord) (*LocalStore, error) {
	store := new(LocalStore)
	cacheRoot = strings.TrimSpace(cacheRoot)
	if cacheRoot == "" {
		return nil, fmt.Errorf("cache root is required")
	}
	store.CacheRoot = cacheRoot
	store.certRoot = store.CertificatesRoot()
	store.bundleFetcher = bundleFetcher
	store.dnsChan = dnsChan
	return store, nil
}

func (s *LocalStore) Start() error {
	if err := os.MkdirAll(s.CacheRoot, 0o755); err != nil {
		return err
	}
	if err := os.MkdirAll(s.certRoot, 0o755); err != nil {
		return err
	}
	db, err := gorm.Open(sqlite.Open(s.MetadataDBPath()), &gorm.Config{})
	if err != nil {
		return err
	}
	s.db = db
	if err := s.initSchema(); err != nil {
		return err
	}
	return nil
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
