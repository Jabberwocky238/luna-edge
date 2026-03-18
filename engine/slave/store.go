package slave

import (
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

}

type LocalStore struct {
	CacheRoot string
	certRoot  string
	db        *gorm.DB
}

func (s *LocalStore) MetadataDBPath() string {
	return filepath.Join(s.CacheRoot, DefaultMetadataDBName)
}

func (s *LocalStore) CertificatesRoot() string {
	return filepath.Join(s.CacheRoot, DefaultCertificatesDir)
}

func NewLocalStore(cacheRoot string) (engine.Client, error) {
	store := new(LocalStore)
	cacheRoot = strings.TrimSpace(cacheRoot)
	if cacheRoot == "" {
		return nil, fmt.Errorf("cache root is required")
	}
	store.CacheRoot = cacheRoot
	if err := os.MkdirAll(store.CacheRoot, 0o755); err != nil {
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
