package slave

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"strings"

	"github.com/jabberwocky238/luna-edge/ingress"
	"github.com/jabberwocky238/luna-edge/replication"
	"github.com/jabberwocky238/luna-edge/utils"
)

func (s *LocalStore) PutCertificateBundle(ctx context.Context, bundle *replication.CertificateBundle) error {
	if bundle == nil {
		return errors.New("bundle is nil")
	}
	if err := writeCertificateBundle(s.certRoot, bundle); err != nil {
		utils.CertLogf("slave-store: write certificate bundle failed hostname=%s revision=%d err=%v", bundle.Hostname, bundle.Revision, err)
		return err
	}
	utils.CertLogf("slave-store: write certificate bundle done hostname=%s revision=%d", bundle.Hostname, bundle.Revision)
	return nil
}

func (s *LocalStore) CheckCertificateBundle(ctx context.Context, hostname string, revision uint64) (bool, error) {
	if s == nil || strings.TrimSpace(hostname) == "" {
		return false, fmt.Errorf("hostname is required")
	}
	dir := filepath.Join(s.certRoot, ingress.CertificateDirectoryName(hostname))
	var err error
	_, err = os.Stat(filepath.Join(dir, "tls.crt"))
	if os.IsNotExist(err) {
		return false, nil
	}
	_, err = os.Stat(filepath.Join(dir, "tls.key"))
	if os.IsNotExist(err) {
		return false, nil
	}
	_, err = os.Stat(filepath.Join(dir, "metadata.json"))
	if os.IsNotExist(err) {
		return false, nil
	}
	if err != nil {
		return false, errors.New("failed to stat certificate files: " + err.Error())
	}
	return true, nil
}

func (s *LocalStore) GetCertificateBundle(_ context.Context, hostname string, revision uint64) (*replication.CertificateBundle, error) {
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
	bundle := &replication.CertificateBundle{
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

func writeCertificateBundle(certRoot string, bundle *replication.CertificateBundle) error {
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
