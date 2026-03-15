package functions

import (
	"context"

	"github.com/jabberwocky238/luna-edge/repository/metadata"
	"gorm.io/gorm"
)

func (r *GormRepository) DNSProjections() GenericRepository[*metadata.DNSProjection] {
	return &gormGenericRepository[*metadata.DNSProjection]{db: r.db}
}

func (r *GormRepository) DNSRecords() GenericRepository[*metadata.DNSRecord] {
	return &gormGenericRepository[*metadata.DNSRecord]{db: r.db}
}

func (r *GormRepository) ReplaceDNSProjection(ctx context.Context, projection *metadata.DNSProjection, records []metadata.DNSRecord) error {
	return r.db.WithContext(ctx).Transaction(func(tx *gorm.DB) error {
		repo := &gormGenericRepository[*metadata.DNSProjection]{db: tx}
		if err := repo.UpsertResource(ctx, projection); err != nil {
			return err
		}
		if err := tx.Where("domain_id = ?", projection.DomainID).Delete(&metadata.DNSRecord{}).Error; err != nil {
			return err
		}
		if len(records) == 0 {
			return nil
		}
		return tx.Create(&records).Error
	})
}

func (r *GormRepository) GetDNSProjection(ctx context.Context, domainID string) (*metadata.DNSProjection, error) {
	projection := &metadata.DNSProjection{}
	if err := r.DNSProjections().GetResourceByField(ctx, projection, "domain_id", domainID); err != nil {
		return nil, err
	}
	return projection, nil
}

func (r *GormRepository) ListDNSRecordsByQuestion(ctx context.Context, fqdn, recordType string) ([]metadata.DNSRecord, error) {
	var records []metadata.DNSRecord
	err := r.db.WithContext(ctx).
		Order("fqdn asc, record_type asc, id asc").
		Find(&records, "fqdn = ? AND record_type = ? AND enabled = ?", fqdn, recordType, true).Error
	return records, err
}

func (r *GormRepository) ListDNSRecordsByDomainID(ctx context.Context, domainID string) ([]metadata.DNSRecord, error) {
	var records []metadata.DNSRecord
	err := r.db.WithContext(ctx).Order("fqdn asc, record_type asc").Find(&records, "domain_id = ?", domainID).Error
	return records, err
}
