package slave

import (
	"context"
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"slices"
	"strings"

	"github.com/jabberwocky238/luna-edge/engine"
	"github.com/jabberwocky238/luna-edge/ingress"
	"github.com/jabberwocky238/luna-edge/repository"
	"github.com/jabberwocky238/luna-edge/repository/connection"
	"github.com/jabberwocky238/luna-edge/repository/metadata"
	"gorm.io/gorm"
)

const (
	DefaultMetadataDBName  = "meta.db"
	DefaultCertificatesDir = "certs"
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
	factory   repository.Factory
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
	factory, err := repository.NewFactory(connection.Config{
		Driver:      connection.DriverSQLite,
		Path:        store.MetadataDBPath(),
		AutoMigrate: true,
	})
	if err != nil {
		return nil, err
	}
	store.factory = factory
	store.db = factory.Connection().DB()
	return store, nil
}

func (s *LocalStore) Close() error {
	if s == nil || s.factory == nil {
		return nil
	}
	return s.factory.Close()
}

func (s *LocalStore) Repository() repository.Repository {
	if s == nil || s.factory == nil {
		return nil
	}
	return s.factory.Repository()
}

func (s *LocalStore) SetCertificateBundleFetcher(fetcher CertificateBundleFetcher) {
	if s == nil {
		return
	}
	s.fetcher = fetcher
}

func (s *LocalStore) SyncSnapshotCertificates(ctx context.Context, snapshot *engine.Snapshot) error {
	if s == nil || snapshot == nil {
		return nil
	}
	activeHosts := make([]string, 0, len(snapshot.Certificates))
	for i := range snapshot.Certificates {
		activeHosts = append(activeHosts, snapshot.Certificates[i].Hostname)
		if err := s.SyncCertificateBundle(ctx, &snapshot.Certificates[i]); err != nil {
			return err
		}
	}
	return s.pruneInactiveCertificates(activeHosts)
}

func (s *LocalStore) SyncCertificateBundle(ctx context.Context, cert *engine.CertificateRecord) error {
	if s == nil || cert == nil || strings.TrimSpace(cert.Hostname) == "" || strings.TrimSpace(s.certRoot) == "" {
		return nil
	}
	if s.fetcher == nil {
		return nil
	}
	bundle, err := s.fetcher.FetchCertificateBundle(ctx, cert.Hostname, cert.Revision)
	if err != nil {
		return err
	}
	return writeCertificateBundle(s.certRoot, bundle)
}

func (s *LocalStore) GetRouteByHostname(ctx context.Context, hostname string) (*engine.RouteRecord, error) {
	var route metadata.RouteProjection
	if err := s.db.WithContext(ctx).First(&route, "hostname = ?", hostname).Error; err != nil {
		return nil, err
	}
	var binding metadata.ServiceBinding
	if err := s.db.WithContext(ctx).First(&binding, "domain_id = ?", route.DomainID).Error; err != nil {
		return nil, err
	}
	var status metadata.DomainEndpointStatus
	_ = s.db.WithContext(ctx).First(&status, "domain_endpoint_id = ?", route.DomainID).Error
	var attachment metadata.Attachment
	_ = s.db.WithContext(ctx).Order("updated_at desc").First(&attachment, "domain_id = ?", route.DomainID).Error
	return &engine.RouteRecord{
		DomainID:            route.DomainID,
		Hostname:            route.Hostname,
		BindingID:           route.BindingID,
		RouteVersion:        route.RouteVersion,
		CertificateRevision: status.CertificateRevision,
		Listener:            attachment.Listener,
		Protocol:            string(route.Protocol),
		UpstreamAddress:     binding.Address,
		UpstreamPort:        binding.Port,
		UpstreamProtocol:    string(binding.Protocol),
		BackendJSON:         binding.BackendJSON,
	}, nil
}

func (s *LocalStore) GetBindingByHostname(ctx context.Context, hostname string) (*engine.BindingRecord, error) {
	var binding metadata.ServiceBinding
	if err := s.db.WithContext(ctx).First(&binding, "hostname = ?", hostname).Error; err != nil {
		return nil, err
	}
	return &engine.BindingRecord{
		ID:           binding.ID,
		DomainID:     binding.DomainID,
		Hostname:     binding.Hostname,
		ServiceID:    binding.ServiceID,
		Namespace:    binding.Namespace,
		Name:         binding.Name,
		Address:      binding.Address,
		Port:         binding.Port,
		Protocol:     string(binding.Protocol),
		RouteVersion: binding.RouteVersion,
		BackendJSON:  binding.BackendJSON,
	}, nil
}

func (s *LocalStore) GetCertificate(ctx context.Context, hostname string, revision uint64) (*engine.CertificateRecord, error) {
	var cert metadata.CertificateRevision
	if err := s.db.WithContext(ctx).First(&cert, "hostname = ?", hostname).Error; err != nil {
		return nil, err
	}
	if revision != 0 && cert.Revision != revision {
		return nil, gorm.ErrRecordNotFound
	}
	return &engine.CertificateRecord{
		ID:             cert.ID,
		DomainID:       cert.DomainID,
		ZoneID:         cert.ZoneID,
		Hostname:       cert.Hostname,
		Revision:       cert.Revision,
		Status:         string(cert.Status),
		ArtifactBucket: cert.ArtifactBucket,
		ArtifactPrefix: cert.ArtifactPrefix,
		SHA256Crt:      cert.SHA256Crt,
		SHA256Key:      cert.SHA256Key,
		NotBefore:      cert.NotBefore,
		NotAfter:       cert.NotAfter,
	}, nil
}

func (s *LocalStore) ListAssignments(ctx context.Context, nodeID string) ([]engine.AssignmentRecord, error) {
	var attachments []metadata.Attachment
	if err := s.db.WithContext(ctx).Order("domain_id asc").Find(&attachments, "node_id = ?", nodeID).Error; err != nil {
		return nil, err
	}
	records := make([]engine.AssignmentRecord, 0, len(attachments))
	for _, attachment := range attachments {
		var domain metadata.DomainEndpoint
		_ = s.db.WithContext(ctx).First(&domain, "id = ?", attachment.DomainID).Error
		var route metadata.RouteProjection
		_ = s.db.WithContext(ctx).First(&route, "domain_id = ?", attachment.DomainID).Error
		records = append(records, engine.AssignmentRecord{
			ID:                         attachment.ID,
			NodeID:                     attachment.NodeID,
			DomainID:                   attachment.DomainID,
			Hostname:                   domain.Hostname,
			Listener:                   attachment.Listener,
			BindingID:                  route.BindingID,
			DesiredRouteVersion:        attachment.DesiredRouteVersion,
			DesiredCertificateRevision: attachment.DesiredCertificateRevision,
			DesiredDNSVersion:          attachment.DesiredDNSVersion,
			State:                      attachment.State,
			LastError:                  attachment.LastError,
		})
	}
	return records, nil
}

func (s *LocalStore) GetVersions(ctx context.Context, nodeID string) (engine.VersionVector, error) {
	var attachments []metadata.Attachment
	if err := s.db.WithContext(ctx).Find(&attachments, "node_id = ?", nodeID).Error; err != nil {
		return engine.VersionVector{}, err
	}
	var versions engine.VersionVector
	for i := range attachments {
		if attachments[i].DesiredRouteVersion > versions.DesiredRouteVersion {
			versions.DesiredRouteVersion = attachments[i].DesiredRouteVersion
		}
		if attachments[i].DesiredCertificateRevision > versions.DesiredCertificateRevision {
			versions.DesiredCertificateRevision = attachments[i].DesiredCertificateRevision
		}
		if attachments[i].DesiredDNSVersion > versions.DesiredDNSVersion {
			versions.DesiredDNSVersion = attachments[i].DesiredDNSVersion
		}
	}
	return versions, nil
}

func (s *LocalStore) ApplySnapshot(ctx context.Context, snapshot *engine.Snapshot) error {
	if snapshot == nil {
		return nil
	}
	return s.db.WithContext(ctx).Transaction(func(tx *gorm.DB) error {
		for _, model := range []any{
			&metadata.Attachment{},
			&metadata.CertificateRevision{},
			&metadata.ServiceBinding{},
			&metadata.RouteProjection{},
			&metadata.DomainEndpointStatus{},
			&metadata.DomainEndpoint{},
		} {
			if err := tx.Session(&gorm.Session{AllowGlobalUpdate: true}).Delete(model).Error; err != nil {
				return err
			}
		}
		for i := range snapshot.Routes {
			if err := upsertRoute(tx, &snapshot.Routes[i]); err != nil {
				return err
			}
		}
		for i := range snapshot.Bindings {
			if err := upsertBinding(tx, &snapshot.Bindings[i]); err != nil {
				return err
			}
		}
		for i := range snapshot.Certificates {
			if err := upsertCertificate(tx, &snapshot.Certificates[i]); err != nil {
				return err
			}
		}
		for i := range snapshot.Assignments {
			if err := upsertAssignment(tx, &snapshot.Assignments[i]); err != nil {
				return err
			}
		}
		return nil
	})
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

func upsertRoute(tx *gorm.DB, route *engine.RouteRecord) error {
	if route == nil {
		return nil
	}
	if err := tx.Save(&metadata.DomainEndpoint{
		ID:           route.DomainID,
		Hostname:     route.Hostname,
		StateVersion: route.RouteVersion,
	}).Error; err != nil {
		return err
	}
	return tx.Save(&metadata.RouteProjection{
		DomainID:     route.DomainID,
		Hostname:     route.Hostname,
		RouteVersion: route.RouteVersion,
		Protocol:     metadata.ServiceBindingRouteKind(route.Protocol),
		RouteJSON:    route.BackendJSON,
		BindingID:    route.BindingID,
	}).Error
}

func upsertBinding(tx *gorm.DB, binding *engine.BindingRecord) error {
	if binding == nil {
		return nil
	}
	return tx.Save(&metadata.ServiceBinding{
		ID:           binding.ID,
		DomainID:     binding.DomainID,
		Hostname:     binding.Hostname,
		ServiceID:    binding.ServiceID,
		Namespace:    binding.Namespace,
		Name:         binding.Name,
		Address:      binding.Address,
		Port:         binding.Port,
		Protocol:     metadata.ServiceBindingRouteKind(binding.Protocol),
		RouteVersion: binding.RouteVersion,
		BackendJSON:  binding.BackendJSON,
	}).Error
}

func upsertCertificate(tx *gorm.DB, cert *engine.CertificateRecord) error {
	if cert == nil {
		return nil
	}
	if err := tx.Where("domain_id = ? OR hostname = ?", cert.DomainID, cert.Hostname).Delete(&metadata.CertificateRevision{}).Error; err != nil {
		return err
	}
	if err := tx.Save(&metadata.CertificateRevision{
		ID:             cert.ID,
		DomainID:       cert.DomainID,
		ZoneID:         cert.ZoneID,
		Hostname:       cert.Hostname,
		Revision:       cert.Revision,
		ArtifactBucket: cert.ArtifactBucket,
		ArtifactPrefix: cert.ArtifactPrefix,
		SHA256Crt:      cert.SHA256Crt,
		SHA256Key:      cert.SHA256Key,
		Status:         metadata.CertificateRevisionStatus(cert.Status),
		NotBefore:      cert.NotBefore,
		NotAfter:       cert.NotAfter,
	}).Error; err != nil {
		return err
	}
	return tx.Save(&metadata.DomainEndpointStatus{
		DomainEndpointID:    cert.DomainID,
		CertificateRevision: cert.Revision,
		CertificateReady:    true,
		Ready:               true,
		Phase:               metadata.DomainPhaseReady,
	}).Error
}

func upsertAssignment(tx *gorm.DB, assignment *engine.AssignmentRecord) error {
	if assignment == nil {
		return nil
	}
	if err := tx.Save(&metadata.DomainEndpoint{
		ID:       assignment.DomainID,
		Hostname: assignment.Hostname,
	}).Error; err != nil {
		return err
	}
	return tx.Save(&metadata.Attachment{
		ID:                         assignment.ID,
		DomainID:                   assignment.DomainID,
		NodeID:                     assignment.NodeID,
		Listener:                   assignment.Listener,
		DesiredCertificateRevision: assignment.DesiredCertificateRevision,
		DesiredRouteVersion:        assignment.DesiredRouteVersion,
		DesiredDNSVersion:          assignment.DesiredDNSVersion,
		State:                      assignment.State,
		LastError:                  assignment.LastError,
	}).Error
}
