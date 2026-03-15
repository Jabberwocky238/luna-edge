package dns

import (
	"fmt"
	"sync"

	"github.com/jabberwocky238/luna-edge/repository/metadata"
)

type dnsMemoryStore struct {
	mu       sync.RWMutex
	byKey    map[string][]metadata.DNSRecord
	byDomain map[string]map[string]metadata.DNSRecord
}

func newDNSMemoryStore() *dnsMemoryStore {
	return &dnsMemoryStore{
		byKey:    map[string][]metadata.DNSRecord{},
		byDomain: map[string]map[string]metadata.DNSRecord{},
	}
}

func (s *dnsMemoryStore) Lookup(fqdn, recordType string) ([]metadata.DNSRecord, bool) {
	s.mu.RLock()
	defer s.mu.RUnlock()
	records, ok := s.byKey[dnsCacheKey(fqdn, recordType)]
	if !ok {
		return nil, false
	}
	return cloneDNSRecords(records), true
}

func (s *dnsMemoryStore) Restore(records []metadata.DNSRecord) {
	s.mu.Lock()
	defer s.mu.Unlock()

	s.byKey = map[string][]metadata.DNSRecord{}
	s.byDomain = map[string]map[string]metadata.DNSRecord{}
	for _, record := range records {
		s.upsertLocked(record)
	}
}

func (s *dnsMemoryStore) Add(record metadata.DNSRecord) {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.upsertLocked(record)
}

func (s *dnsMemoryStore) Modify(domainID, recordID string, apply func(*metadata.DNSRecord) error) (*metadata.DNSRecord, error) {
	s.mu.Lock()
	defer s.mu.Unlock()

	domainRecords := s.byDomain[domainID]
	if domainRecords == nil {
		return nil, fmt.Errorf("domain %q not found", domainID)
	}
	record, ok := domainRecords[recordID]
	if !ok {
		return nil, fmt.Errorf("dns record %q not found", recordID)
	}
	delete(domainRecords, recordID)
	s.rebuildQuestionLocked(record.FQDN, record.RecordType)
	if err := apply(&record); err != nil {
		return nil, err
	}
	s.upsertLocked(record)
	recordCopy := record
	return &recordCopy, nil
}

func (s *dnsMemoryStore) Delete(domainID, recordID string) (*metadata.DNSRecord, error) {
	s.mu.Lock()
	defer s.mu.Unlock()

	domainRecords := s.byDomain[domainID]
	if domainRecords == nil {
		return nil, fmt.Errorf("domain %q not found", domainID)
	}
	record, ok := domainRecords[recordID]
	if !ok {
		return nil, fmt.Errorf("dns record %q not found", recordID)
	}
	delete(domainRecords, recordID)
	if len(domainRecords) == 0 {
		delete(s.byDomain, domainID)
	}
	s.rebuildQuestionLocked(record.FQDN, record.RecordType)
	recordCopy := record
	return &recordCopy, nil
}

func (s *dnsMemoryStore) RefreshQuestion(fqdn, recordType string) {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.rebuildQuestionLocked(fqdn, recordType)
}

func (s *dnsMemoryStore) Clear() {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.byKey = map[string][]metadata.DNSRecord{}
	s.byDomain = map[string]map[string]metadata.DNSRecord{}
}

func (s *dnsMemoryStore) upsertLocked(record metadata.DNSRecord) {
	record.FQDN = normalizeFQDN(record.FQDN)
	record.RecordType = normalizeRecordType(record.RecordType)
	if s.byDomain[record.DomainID] == nil {
		s.byDomain[record.DomainID] = map[string]metadata.DNSRecord{}
	}
	s.byDomain[record.DomainID][record.ID] = record
	s.rebuildQuestionLocked(record.FQDN, record.RecordType)
}

func (s *dnsMemoryStore) rebuildQuestionLocked(fqdn, recordType string) {
	key := dnsCacheKey(fqdn, recordType)
	normalizedFQDN := normalizeFQDN(fqdn)
	normalizedType := normalizeRecordType(recordType)
	var records []metadata.DNSRecord
	for _, domainRecords := range s.byDomain {
		for _, record := range domainRecords {
			if !record.Enabled {
				continue
			}
			if normalizeFQDN(record.FQDN) != normalizedFQDN {
				continue
			}
			if normalizeRecordType(record.RecordType) != normalizedType {
				continue
			}
			records = append(records, record)
		}
	}
	if len(records) == 0 {
		delete(s.byKey, key)
		return
	}
	s.byKey[key] = records
}

func cloneDNSRecords(records []metadata.DNSRecord) []metadata.DNSRecord {
	out := make([]metadata.DNSRecord, len(records))
	copy(out, records)
	return out
}

func dnsCacheKey(fqdn, recordType string) string {
	return normalizeFQDN(fqdn) + "|" + normalizeRecordType(recordType)
}
