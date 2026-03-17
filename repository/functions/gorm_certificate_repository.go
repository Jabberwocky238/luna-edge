package functions

import (
	"context"

	"github.com/jabberwocky238/luna-edge/repository/metadata"
)

func (r *GormRepository) CertificateRevisions() GenericRepository[*metadata.CertificateRevision] {
	return &gormGenericRepository[*metadata.CertificateRevision]{db: r.db}
}

func (r *GormRepository) UpsertCertificateRevision(ctx context.Context, revision *metadata.CertificateRevision) error {
	return r.CertificateRevisions().UpsertResource(ctx, revision)
}

func (r *GormRepository) GetCertificateRevision(ctx context.Context, domainID string, revision uint64) (*metadata.CertificateRevision, error) {
	cert := &metadata.CertificateRevision{}
	if err := r.db.WithContext(ctx).
		Where("deleted = ?", false).
		First(cert, "domain_endpoint_id = ? AND revision = ?", domainID, revision).Error; err != nil {
		return nil, err
	}
	return cert, nil
}

func (r *GormRepository) GetLatestCertificateRevision(ctx context.Context, domainID string) (*metadata.CertificateRevision, error) {
	cert := &metadata.CertificateRevision{}
	if err := r.db.WithContext(ctx).
		Where("deleted = ?", false).
		Order("revision desc").
		First(cert, "domain_endpoint_id = ?", domainID).Error; err != nil {
		return nil, err
	}
	return cert, nil
}

func (r *GormRepository) ListCertificateRevisions(ctx context.Context, domainID string) ([]metadata.CertificateRevision, error) {
	var certs []metadata.CertificateRevision
	err := r.db.WithContext(ctx).
		Where("deleted = ?", false).
		Order("revision desc").
		Find(&certs, "domain_endpoint_id = ?", domainID).Error
	return certs, err
}
