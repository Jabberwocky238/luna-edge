package functions

import (
	"context"
	"strings"

	"github.com/jabberwocky238/luna-edge/repository/metadata"
	"gorm.io/gorm"
)

func (r *GormRepository) CertificateRevisions() GenericRepository[*metadata.CertificateRevision] {
	return &GormGenericRepository[*metadata.CertificateRevision]{db: r.db}
}

func (r *GormRepository) UpsertCertificateRevision(ctx context.Context, revision *metadata.CertificateRevision) error {
	return r.CertificateRevisions().UpsertResource(ctx, revision)
}

func (r *GormRepository) GetLatestCertificateRevision(ctx context.Context, hostname string) (*metadata.CertificateRevision, error) {
	cert := &metadata.CertificateRevision{}
	if err := r.db.WithContext(ctx).
		Where("deleted = ?", false).
		Order("revision desc").
		First(cert, "hostname = ?", hostname).Error; err != nil {
		return nil, err
	}
	return cert, nil
}

func (r *GormRepository) GetActiveCertificateForDomain(ctx context.Context, domain *metadata.DomainEndpoint) (*metadata.CertificateRevision, error) {
	if domain == nil {
		return nil, nil
	}
	hostname := strings.TrimSpace(domain.Hostname)
	if hostname == "" {
		return nil, nil
	}
	cert := &metadata.CertificateRevision{}
	err := r.db.WithContext(ctx).
		Where("deleted = ?", false).
		Where("hostname = ?", hostname).
		Order("revision desc").
		First(cert).Error
	if err != nil {
		if err == gorm.ErrRecordNotFound {
			return nil, nil
		}
		return nil, err
	}
	return cert, nil
}

func (r *GormRepository) ListCertificateRevisions(ctx context.Context, hostname string) ([]metadata.CertificateRevision, error) {
	var certs []metadata.CertificateRevision
	err := r.db.WithContext(ctx).
		Where("deleted = ?", false).
		Order("revision desc").
		Find(&certs, "hostname = ?", hostname).Error
	return certs, err
}
