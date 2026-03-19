package functions

import (
	"context"

	"github.com/jabberwocky238/luna-edge/repository/metadata"
)

func (r *GormRepository) DNSRecords() GenericRepository[*metadata.DNSRecord] {
	return &GormGenericRepository[*metadata.DNSRecord]{db: r.db}
}

func (r *GormRepository) ListDNSRecordsByQuestion(ctx context.Context, fqdn, recordType string) ([]metadata.DNSRecord, error) {
	var records []metadata.DNSRecord
	err := r.db.WithContext(ctx).
		Where("deleted = ?", false).
		Order("fqdn asc, record_type asc, id asc").
		Find(&records, "fqdn = ? AND record_type = ? AND enabled = ?", fqdn, recordType, true).Error
	return records, err
}

func (r *GormRepository) ListDNSRecordsByHostname(ctx context.Context, hostname string) ([]metadata.DNSRecord, error) {
	var records []metadata.DNSRecord
	err := r.db.WithContext(ctx).
		Where("deleted = ?", false).
		Order("fqdn asc, record_type asc").
		Find(&records, "hostname = ?", hostname).Error
	return records, err
}
