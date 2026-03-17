package slave

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"log"

	"github.com/jabberwocky238/luna-edge/engine"
	"github.com/jabberwocky238/luna-edge/repository/metadata"
	"gorm.io/gorm"
	"gorm.io/gorm/clause"
)

type dnsRecordCacheRow struct {
	ID         string `gorm:"column:id;primaryKey;type:text"`
	FQDN       string `gorm:"column:fqdn;not null;index:idx_dns_records_cache_lookup,priority:1;type:text"`
	RecordType string `gorm:"column:record_type;not null;index:idx_dns_records_cache_lookup,priority:2;type:text"`
	DetailJSON string `gorm:"column:detail_json;not null;type:text"`
}

func (dnsRecordCacheRow) TableName() string { return "dns_records_cache" }

type domainEntryCacheRow struct {
	ID           string `gorm:"column:id;primaryKey;type:text"`
	Hostname     string `gorm:"column:hostname;not null;uniqueIndex;index:idx_domain_entries_cache_hostname;type:text"`
	CertHostname string `gorm:"column:cert_hostname;not null;default:'';type:text"`
	CertRevision uint64 `gorm:"column:cert_revision;not null;default:0"`
	DetailJSON   string `gorm:"column:detail_json;not null;type:text"`
}

func (domainEntryCacheRow) TableName() string { return "domain_entries_cache" }

type syncStateRow struct {
	Key   string `gorm:"column:key;primaryKey;type:text"`
	Value string `gorm:"column:value;not null;type:text"`
}

func (syncStateRow) TableName() string { return "sync_state" }

func (s *LocalStore) initSchema() error {
	return s.db.AutoMigrate(&dnsRecordCacheRow{}, &domainEntryCacheRow{}, &syncStateRow{})
}

func (s *LocalStore) ApplySnapshot(ctx context.Context, snapshot *engine.Snapshot) error {
	if snapshot == nil {
		return nil
	}
	log.Printf("slave-store: apply snapshot begin snapshot_record_id=%d last=%v dns=%d domains=%d", snapshot.SnapshotRecordID, snapshot.Last, len(snapshot.DNSRecords), len(snapshot.DomainEntries))
	tx := s.db.WithContext(ctx).Begin()
	err := tx.Error
	if err != nil {
		return err
	}
	defer func() {
		if err != nil {
			_ = tx.Rollback().Error
		}
	}()

	for i := range snapshot.DNSRecords {
		if snapshot.DNSRecords[i].Deleted {
			if execErr := tx.Delete(&dnsRecordCacheRow{}, "id = ?", snapshot.DNSRecords[i].ID).Error; execErr != nil {
				err = execErr
				log.Printf("slave-store: delete dns row failed snapshot_record_id=%d dns_id=%s err=%v", snapshot.SnapshotRecordID, snapshot.DNSRecords[i].ID, execErr)
				return err
			}
			log.Printf("slave-store: delete dns row snapshot_record_id=%d dns_id=%s", snapshot.SnapshotRecordID, snapshot.DNSRecords[i].ID)
			continue
		}
		payload, marshalErr := json.Marshal(snapshot.DNSRecords[i])
		if marshalErr != nil {
			err = marshalErr
			return err
		}
		row := &dnsRecordCacheRow{
			ID:         snapshot.DNSRecords[i].ID,
			FQDN:       snapshot.DNSRecords[i].FQDN,
			RecordType: string(snapshot.DNSRecords[i].RecordType),
			DetailJSON: string(payload),
		}
		if execErr := tx.Clauses(clause.OnConflict{UpdateAll: true}).Create(row).Error; execErr != nil {
			err = execErr
			log.Printf("slave-store: upsert dns row failed snapshot_record_id=%d dns_id=%s err=%v", snapshot.SnapshotRecordID, row.ID, execErr)
			return err
		}
		log.Printf("slave-store: upsert dns row snapshot_record_id=%d dns_id=%s fqdn=%s", snapshot.SnapshotRecordID, row.ID, row.FQDN)
	}

	for i := range snapshot.DomainEntries {
		if snapshot.DomainEntries[i].Deleted {
			if execErr := tx.Delete(&domainEntryCacheRow{}, "hostname = ?", snapshot.DomainEntries[i].Hostname).Error; execErr != nil {
				err = execErr
				log.Printf("slave-store: delete domain row failed snapshot_record_id=%d hostname=%s err=%v", snapshot.SnapshotRecordID, snapshot.DomainEntries[i].Hostname, execErr)
				return err
			}
			log.Printf("slave-store: delete domain row snapshot_record_id=%d hostname=%s", snapshot.SnapshotRecordID, snapshot.DomainEntries[i].Hostname)
			continue
		}
		payload, marshalErr := json.Marshal(snapshot.DomainEntries[i])
		if marshalErr != nil {
			err = marshalErr
			return err
		}
		certHostname := ""
		var certRevision uint64
		if snapshot.DomainEntries[i].Cert != nil {
			certHostname = snapshot.DomainEntries[i].Cert.Hostname
			certRevision = snapshot.DomainEntries[i].Cert.Revision
		}
		row := &domainEntryCacheRow{
			ID:           snapshot.DomainEntries[i].ID,
			Hostname:     snapshot.DomainEntries[i].Hostname,
			CertHostname: certHostname,
			CertRevision: certRevision,
			DetailJSON:   string(payload),
		}
		if execErr := tx.Clauses(clause.OnConflict{UpdateAll: true}).Create(row).Error; execErr != nil {
			err = execErr
			log.Printf("slave-store: upsert domain row failed snapshot_record_id=%d domain_id=%s err=%v", snapshot.SnapshotRecordID, row.ID, execErr)
			return err
		}
		log.Printf("slave-store: upsert domain row snapshot_record_id=%d domain_id=%s hostname=%s cert_revision=%d", snapshot.SnapshotRecordID, row.ID, row.Hostname, row.CertRevision)
	}

	if snapshot.Last {
		row := &syncStateRow{Key: snapshotCursorStateKey, Value: fmt.Sprintf("%d", snapshot.SnapshotRecordID)}
		if execErr := tx.Clauses(clause.OnConflict{UpdateAll: true}).Create(row).Error; execErr != nil {
			err = execErr
			log.Printf("slave-store: update cursor failed snapshot_record_id=%d err=%v", snapshot.SnapshotRecordID, execErr)
			return err
		}
		log.Printf("slave-store: cursor updated snapshot_record_id=%d", snapshot.SnapshotRecordID)
	}

	err = tx.Commit().Error
	if err != nil {
		log.Printf("slave-store: apply snapshot commit failed snapshot_record_id=%d err=%v", snapshot.SnapshotRecordID, err)
		return err
	}
	log.Printf("slave-store: apply snapshot done snapshot_record_id=%d", snapshot.SnapshotRecordID)
	return err
}

func (s *LocalStore) ListDNSRecords(ctx context.Context) ([]metadata.DNSRecord, error) {
	var cacheRows []dnsRecordCacheRow
	if err := s.db.WithContext(ctx).Order("fqdn asc, record_type asc, id asc").Find(&cacheRows).Error; err != nil {
		return nil, err
	}
	var out []metadata.DNSRecord
	for i := range cacheRows {
		var record metadata.DNSRecord
		if err := json.Unmarshal([]byte(cacheRows[i].DetailJSON), &record); err != nil {
			return nil, err
		}
		out = append(out, record)
	}
	return out, nil
}

func (s *LocalStore) GetDNSRecordsByHostname(ctx context.Context, hostname string) ([]metadata.DNSRecord, error) {
	var cacheRows []dnsRecordCacheRow
	if err := s.db.WithContext(ctx).
		Order("record_type asc, id asc").
		Find(&cacheRows, "fqdn = ?", hostname).Error; err != nil {
		return nil, err
	}
	out := make([]metadata.DNSRecord, 0, len(cacheRows))
	for i := range cacheRows {
		var record metadata.DNSRecord
		if err := json.Unmarshal([]byte(cacheRows[i].DetailJSON), &record); err != nil {
			return nil, err
		}
		out = append(out, record)
	}
	return out, nil
}

func (s *LocalStore) GetSnapshotRecordID(ctx context.Context) (uint64, error) {
	var row syncStateRow
	if err := s.db.WithContext(ctx).First(&row, "key = ?", snapshotCursorStateKey).Error; err != nil {
		if errors.Is(err, gorm.ErrRecordNotFound) {
			return 0, nil
		}
		return 0, err
	}
	var out uint64
	_, err := fmt.Sscanf(row.Value, "%d", &out)
	return out, err
}

func (s *LocalStore) GetDomainEntryByHostname(ctx context.Context, hostname string) (*metadata.DomainEntryProjection, error) {
	var row domainEntryCacheRow
	if err := s.db.WithContext(ctx).First(&row, "hostname = ?", hostname).Error; err != nil {
		return nil, err
	}
	var entry metadata.DomainEntryProjection
	if err := json.Unmarshal([]byte(row.DetailJSON), &entry); err != nil {
		return nil, err
	}
	return &entry, nil
}

func serviceDNSName(ref *metadata.ServiceBackendRef) string {
	if ref == nil {
		return ""
	}
	if ref.ServiceNamespace == "" {
		return ref.ServiceName
	}
	return fmt.Sprintf("%s.%s.svc.cluster.local", ref.ServiceName, ref.ServiceNamespace)
}

func protocolForBackendType(kind metadata.BackendType) string {
	switch kind {
	case metadata.BackendTypeL7HTTPS:
		return "https"
	case metadata.BackendTypeL4TLSPassthrough, metadata.BackendTypeL4TLSTermination:
		return "tcp"
	default:
		return "http"
	}
}
