package functions

import (
	"context"

	"github.com/jabberwocky238/luna-edge/repository/metadata"
)

func (r *GormRepository) DNSRecords() GenericRepository[*metadata.DNSRecord] {
	return &gormGenericRepository[*metadata.DNSRecord]{db: r.db}
}

func (r *GormRepository) ReplaceDNSRecords(ctx context.Context, domainID string, records []metadata.DNSRecord) error {
	if err := r.db.WithContext(ctx).
		Model(&metadata.DNSRecord{}).
		Where("deleted = ?", false).
		Where("domain_endpoint_id = ?", domainID).
		Update("deleted", true).Error; err != nil {
		return err
	}
	if len(records) == 0 {
		return nil
	}
	return r.db.WithContext(ctx).Create(&records).Error
}

func (r *GormRepository) ListDNSRecordsByQuestion(ctx context.Context, fqdn, recordType string) ([]metadata.DNSRecord, error) {
	var records []metadata.DNSRecord
	err := r.db.WithContext(ctx).
		Where("deleted = ?", false).
		Order("fqdn asc, record_type asc, id asc").
		Find(&records, "fqdn = ? AND record_type = ? AND enabled = ?", fqdn, recordType, true).Error
	return records, err
}

func (r *GormRepository) ListDNSRecordsByDomainID(ctx context.Context, domainID string) ([]metadata.DNSRecord, error) {
	var records []metadata.DNSRecord
	err := r.db.WithContext(ctx).
		Where("deleted = ?", false).
		Order("fqdn asc, record_type asc").
		Find(&records, "domain_endpoint_id = ?", domainID).Error
	return records, err
}
