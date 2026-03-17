package slave

import (
	"context"
	"encoding/json"
	"fmt"
	"log"
	"os"
	"path/filepath"
	"slices"
	"strings"

	"github.com/jabberwocky238/luna-edge/engine"
	"github.com/jabberwocky238/luna-edge/ingress"
)

func (s *LocalStore) SyncSnapshotCertificates(ctx context.Context, snapshot *engine.Snapshot) error {
	if s == nil || snapshot == nil {
		return nil
	}
	log.Printf("slave-store: sync certificates begin snapshot_record_id=%d domains=%d", snapshot.SnapshotRecordID, len(snapshot.DomainEntries))
	activeHosts := make([]string, 0, len(snapshot.DomainEntries))
	for i := range snapshot.DomainEntries {
		cert := snapshot.DomainEntries[i].Cert
		if cert == nil || strings.TrimSpace(cert.Hostname) == "" {
			continue
		}
		activeHosts = append(activeHosts, cert.Hostname)
		if err := s.SyncCertificateBundle(ctx, &engine.CertificateBundle{Hostname: cert.Hostname, Revision: cert.Revision}); err != nil {
			log.Printf("slave-store: sync certificate bundle failed snapshot_record_id=%d hostname=%s revision=%d err=%v", snapshot.SnapshotRecordID, cert.Hostname, cert.Revision, err)
			return err
		}
		log.Printf("slave-store: sync certificate bundle done snapshot_record_id=%d hostname=%s revision=%d", snapshot.SnapshotRecordID, cert.Hostname, cert.Revision)
	}
	if err := s.pruneInactiveCertificates(activeHosts); err != nil {
		log.Printf("slave-store: prune inactive certificates failed snapshot_record_id=%d err=%v", snapshot.SnapshotRecordID, err)
		return err
	}
	log.Printf("slave-store: sync certificates done snapshot_record_id=%d active_hosts=%d", snapshot.SnapshotRecordID, len(activeHosts))
	return nil
}

func (s *LocalStore) SyncCertificateBundle(ctx context.Context, cert *engine.CertificateBundle) error {
	if s == nil || cert == nil || strings.TrimSpace(cert.Hostname) == "" || strings.TrimSpace(s.certRoot) == "" {
		return nil
	}
	if s.fetcher == nil {
		log.Printf("slave-store: skip fetch certificate bundle hostname=%s revision=%d reason=no_fetcher", cert.Hostname, cert.Revision)
		return nil
	}
	log.Printf("slave-store: fetch certificate bundle begin hostname=%s revision=%d", cert.Hostname, cert.Revision)
	bundle, err := s.fetcher.FetchCertificateBundle(ctx, cert.Hostname, cert.Revision)
	if err != nil {
		log.Printf("slave-store: fetch certificate bundle failed hostname=%s revision=%d err=%v", cert.Hostname, cert.Revision, err)
		return err
	}
	if err := writeCertificateBundle(s.certRoot, bundle); err != nil {
		log.Printf("slave-store: write certificate bundle failed hostname=%s revision=%d err=%v", cert.Hostname, cert.Revision, err)
		return err
	}
	log.Printf("slave-store: write certificate bundle done hostname=%s revision=%d", cert.Hostname, cert.Revision)
	return nil
}

func (s *LocalStore) GetCertificateBundle(_ context.Context, hostname string, revision uint64) (*engine.CertificateBundle, error) {
	if s == nil || strings.TrimSpace(hostname) == "" {
		return nil, fmt.Errorf("hostname is required")
	}
	dir := filepath.Join(s.certRoot, ingress.CertificateDirectoryName(hostname))
	crt, err := os.ReadFile(filepath.Join(dir, "tls.crt"))
	if err != nil {
		return nil, err
	}
	key, err := os.ReadFile(filepath.Join(dir, "tls.key"))
	if err != nil {
		return nil, err
	}
	metadataJSON, err := os.ReadFile(filepath.Join(dir, "metadata.json"))
	if err != nil && !os.IsNotExist(err) {
		return nil, err
	}
	bundle := &engine.CertificateBundle{
		Hostname:     hostname,
		Revision:     revision,
		TLSCrt:       crt,
		TLSKey:       key,
		MetadataJSON: metadataJSON,
	}
	if len(metadataJSON) > 0 {
		var meta struct {
			Hostname string `json:"hostname"`
			Revision uint64 `json:"revision"`
		}
		if jsonErr := json.Unmarshal(metadataJSON, &meta); jsonErr == nil {
			if strings.TrimSpace(meta.Hostname) != "" {
				bundle.Hostname = meta.Hostname
			}
			if meta.Revision != 0 {
				bundle.Revision = meta.Revision
			}
		}
	}
	return bundle, nil
}

func writeCertificateBundle(certRoot string, bundle *engine.CertificateBundle) error {
	if bundle == nil {
		return fmt.Errorf("certificate bundle is nil")
	}
	dir := filepath.Join(certRoot, ingress.CertificateDirectoryName(bundle.Hostname))
	if err := os.MkdirAll(dir, 0o755); err != nil {
		return err
	}
	if err := os.WriteFile(filepath.Join(dir, "tls.crt"), bundle.TLSCrt, 0o644); err != nil {
		return err
	}
	if err := os.WriteFile(filepath.Join(dir, "tls.key"), bundle.TLSKey, 0o600); err != nil {
		return err
	}
	metadataBytes := bundle.MetadataJSON
	if len(metadataBytes) == 0 {
		var err error
		metadataBytes, err = json.Marshal(map[string]any{
			"hostname": bundle.Hostname,
			"revision": bundle.Revision,
		})
		if err != nil {
			return err
		}
	}
	return os.WriteFile(filepath.Join(dir, "metadata.json"), metadataBytes, 0o644)
}

func removeCertificateFiles(certRoot, hostname string) error {
	if strings.TrimSpace(certRoot) == "" || strings.TrimSpace(hostname) == "" {
		return nil
	}
	err := os.RemoveAll(filepath.Join(certRoot, ingress.CertificateDirectoryName(hostname)))
	if err != nil && !os.IsNotExist(err) {
		return err
	}
	return nil
}

func (s *LocalStore) pruneInactiveCertificates(activeHosts []string) error {
	if s == nil || strings.TrimSpace(s.certRoot) == "" {
		return nil
	}
	entries, err := os.ReadDir(s.certRoot)
	if err != nil {
		if os.IsNotExist(err) {
			return nil
		}
		return err
	}
	for _, entry := range entries {
		if !entry.IsDir() {
			continue
		}
		name := entry.Name()
		if slices.Contains(activeHosts, name) {
			continue
		}
		if err := removeCertificateFiles(s.certRoot, name); err != nil {
			return err
		}
	}
	return nil
}
