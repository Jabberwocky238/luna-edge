package slave

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"log"

	"github.com/jabberwocky238/luna-edge/replication"
	"github.com/jabberwocky238/luna-edge/repository/metadata"
	"github.com/jabberwocky238/luna-edge/utils"
	"gorm.io/gorm"
	"gorm.io/gorm/clause"
)

var ErrSnapshotOutOfOrder = errors.New("snapshot out of order")

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
	SnapshotRecordID uint64 `gorm:"column:snapshot_record_id;primaryKey"`
	AffectedTable    string `gorm:"column:affected_table;primaryKey;type:text"`
	AffectedTableID  string `gorm:"column:value;not null;type:text"`
	CreatedAt        int64  `gorm:"column:created_at;autoCreateTime"`
}

func (syncStateRow) TableName() string { return "sync_state" }

func (s *LocalStore) initSchema() error {
	return s.db.AutoMigrate(&dnsRecordCacheRow{}, &domainEntryCacheRow{}, &syncStateRow{})
}

func (s *LocalStore) ApplySnapshot(ctx context.Context, snapshot *replication.Snapshot) error {
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
		s.dealDNSRecords(ctx, tx, &snapshot.DNSRecords[i], snapshot.SnapshotRecordID)
		s.updateSnapshotRecordID(ctx, tx, snapshot.SnapshotRecordID, "dns_records", snapshot.DNSRecords[i].ID)
	}

	for i := range snapshot.DomainEntries {
		s.dealDomainEntries(ctx, tx, &snapshot.DomainEntries[i], snapshot.SnapshotRecordID)
		s.updateSnapshotRecordID(ctx, tx, snapshot.SnapshotRecordID, "domain_entries", snapshot.DomainEntries[i].ID)
	}

	err = tx.Commit().Error
	if err != nil {
		log.Printf("slave-store: apply snapshot commit failed snapshot_record_id=%d err=%v", snapshot.SnapshotRecordID, err)
		return err
	}
	log.Printf("slave-store: apply snapshot done snapshot_record_id=%d", snapshot.SnapshotRecordID)
	return err
}

func (s *LocalStore) ApplyChangelog(ctx context.Context, changelog *replication.ChangeNotification) error {
	var err error
	if changelog == nil {
		return nil
	}
	cursor, err := s.GetSnapshotRecordID(ctx)
	if cursor >= changelog.SnapshotRecordID {
		log.Printf("slave-store: skip changelog with old snapshot record ID snapshot_record_id=%d current_cursor=%d", changelog.SnapshotRecordID, cursor)
		return nil
	} else if cursor+1 < changelog.SnapshotRecordID {
		log.Printf("slave-store: snapshot record ID out of order snapshot_record_id=%d current_cursor=%d", changelog.SnapshotRecordID, cursor)
		return ErrSnapshotOutOfOrder
	} else {
		// changelog.SnapshotRecordID is exactly cursor+1, which is expected.
	}
	tx := s.db.WithContext(ctx).Begin()
	err = tx.Error
	if err != nil {
		return err
	}
	defer func() {
		if err != nil {
			_ = tx.Rollback().Error
		}
	}()
	if changelog.DNSRecord != nil {
		s.dealDNSRecords(ctx, tx, changelog.DNSRecord, changelog.SnapshotRecordID)
		s.updateSnapshotRecordID(ctx, tx, changelog.SnapshotRecordID, "dns_records", changelog.DNSRecord.ID)
	}
	if changelog.DomainEntry != nil {
		s.dealDomainEntries(ctx, tx, changelog.DomainEntry, changelog.SnapshotRecordID)
		s.updateSnapshotRecordID(ctx, tx, changelog.SnapshotRecordID, "domain_entries", changelog.DomainEntry.ID)
	}
	err = tx.Commit().Error
	if err != nil {
		log.Printf("slave-store: apply changelog commit failed snapshot_record_id=%d err=%v", changelog.SnapshotRecordID, err)
		return err
	}
	return nil
}

func (s *LocalStore) dealDNSRecords(_ context.Context, tx *gorm.DB, input *metadata.DNSRecord, SnapshotRecordID uint64) error {
	if input.Deleted {
		if execErr := tx.Delete(&dnsRecordCacheRow{}, "id = ?", input.ID).Error; execErr != nil {
			log.Printf("slave-store: delete dns row failed snapshot_record_id=%d dns_id=%s err=%v", SnapshotRecordID, input.ID, execErr)
			return execErr
		}
		log.Printf("slave-store: delete dns row snapshot_record_id=%d dns_id=%s", SnapshotRecordID, input.ID)
		return nil
	}
	payload, marshalErr := json.Marshal(input)
	if marshalErr != nil {
		return marshalErr
	}
	row := &dnsRecordCacheRow{
		ID:         input.ID,
		FQDN:       input.FQDN,
		RecordType: string(input.RecordType),
		DetailJSON: string(payload),
	}
	if execErr := tx.Clauses(clause.OnConflict{UpdateAll: true}).Create(row).Error; execErr != nil {
		log.Printf("slave-store: upsert dns row failed snapshot_record_id=%d dns_id=%s err=%v", SnapshotRecordID, row.ID, execErr)
		return execErr
	}
	log.Printf("slave-store: upsert dns row snapshot_record_id=%d dns_id=%s fqdn=%s", SnapshotRecordID, row.ID, row.FQDN)
	s.dnsChan <- []metadata.DNSRecord{*input}
	return nil
}

func (s *LocalStore) dealDomainEntries(ctx context.Context, tx *gorm.DB, input *metadata.DomainEntryProjection, SnapshotRecordID uint64) error {
	if input.Deleted {
		if execErr := tx.Delete(&domainEntryCacheRow{}, "hostname = ?", input.Hostname).Error; execErr != nil {
			log.Printf("slave-store: delete domain row failed snapshot_record_id=%d hostname=%s err=%v", SnapshotRecordID, input.Hostname, execErr)
			return execErr
		}
		log.Printf("slave-store: delete domain row snapshot_record_id=%d hostname=%s", SnapshotRecordID, input.Hostname)
		return nil
	}
	payload, marshalErr := json.Marshal(input)
	if marshalErr != nil {
		return marshalErr
	}
	certHostname := ""
	var certRevision uint64
	if input.Cert != nil {
		certHostname = input.Hostname
		certRevision = input.Cert.Revision
		// check if certificate bundle already exists before fetch,
		// to avoid unnecessary fetch and write when the same cert revision already exists.
		ok, err := s.CheckCertificateBundle(ctx, certHostname, certRevision)
		if err != nil {
			panic("THIS SHOULD NOT HAPPEN: failed to check certificate bundle existence: " + err.Error())
		}
		if ok {
			utils.CertLogf("slave-store: certificate bundle already exists hostname=%s revision=%d", certHostname, certRevision)
		} else {
			utils.CertLogf("slave-store: certificate bundle not exists, starting fetch, hostname=%s revision=%d", certHostname, certRevision)
			go func() {
				bundle, err := s.bundleFetcher.FetchCertificateBundle(ctx, certHostname, certRevision)
				if err != nil || bundle == nil {
					utils.CertLogf("slave-store: get certificate bundle failed hostname=%s revision=%d err=%v", certHostname, certRevision, err)
					return
				}
				if err := s.PutCertificateBundle(ctx, bundle); err != nil {
					utils.CertLogf("slave-store: put certificate bundle failed hostname=%s revision=%d err=%v", certHostname, certRevision, err)
					return
				}
			}()
		}
	}
	row := &domainEntryCacheRow{
		ID:           input.ID,
		Hostname:     input.Hostname,
		CertHostname: certHostname,
		CertRevision: certRevision,
		DetailJSON:   string(payload),
	}
	if execErr := tx.Clauses(clause.OnConflict{UpdateAll: true}).Create(row).Error; execErr != nil {
		log.Printf("slave-store: upsert domain row failed snapshot_record_id=%d domain_id=%s err=%v", SnapshotRecordID, row.ID, execErr)
		return execErr
	}
	log.Printf("slave-store: upsert domain row snapshot_record_id=%d domain_id=%s hostname=%s cert_revision=%d", SnapshotRecordID, row.ID, row.Hostname, row.CertRevision)
	return nil
}

func (s *LocalStore) updateSnapshotRecordID(ctx context.Context, tx *gorm.DB, snapshotRecordID uint64, table string, tableID string) error {
	row := &syncStateRow{
		SnapshotRecordID: snapshotRecordID,
		AffectedTable:    table,
		AffectedTableID:  tableID,
	}
	if execErr := tx.Clauses(clause.OnConflict{UpdateAll: true}).Create(row).Error; execErr != nil {
		log.Printf("slave-store: update snapshot record ID failed snapshot_record_id=%d err=%v", snapshotRecordID, execErr)
		return execErr
	}
	log.Printf("slave-store: update snapshot record ID snapshot_record_id=%d", snapshotRecordID)
	return nil
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
	if err := s.db.WithContext(ctx).Order("snapshot_record_id desc, created_at desc").First(&row).Error; err != nil {
		if errors.Is(err, gorm.ErrRecordNotFound) {
			return 0, nil
		}
		return 0, err
	}
	return row.SnapshotRecordID, nil
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
