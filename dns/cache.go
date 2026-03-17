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

func (s *dnsMemoryStore) Lookup(question DNSQuestion) (*DNSAnswerSet, bool) {
	s.mu.RLock()
	defer s.mu.RUnlock()
	question = normalizeQuestion(question)
	records, ok := s.byKey[dnsCacheKey(question)]
	if !ok {
		return &DNSAnswerSet{
			Question: question,
			Found:    false,
			Records:  nil,
		}, false
	}
	return &DNSAnswerSet{
		Question: question,
		Found:    len(records) > 0,
		Records:  cloneDNSRecords(records),
	}, true
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

func (s *dnsMemoryStore) Add(record metadata.DNSRecord) *DNSAnswerSet {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.upsertLocked(record)
	return s.answerSetLocked(questionFromRecord(record))
}

func (s *dnsMemoryStore) Modify(domainID, recordID string, apply func(*metadata.DNSRecord) error) (*metadata.DNSRecord, *DNSAnswerSet, error) {
	s.mu.Lock()
	defer s.mu.Unlock()

	domainRecords := s.byDomain[domainID]
	if domainRecords == nil {
		return nil, nil, fmt.Errorf("domain %q not found", domainID)
	}
	record, ok := domainRecords[recordID]
	if !ok {
		return nil, nil, fmt.Errorf("dns record %q not found", recordID)
	}
	delete(domainRecords, recordID)
	s.rebuildQuestionLocked(record.FQDN, record.RecordType)
	if err := apply(&record); err != nil {
		return nil, nil, err
	}
	s.upsertLocked(record)
	recordCopy := record
	return &recordCopy, s.answerSetLocked(questionFromRecord(record)), nil
}

func (s *dnsMemoryStore) Delete(domainID, recordID string) (*metadata.DNSRecord, *DNSAnswerSet, error) {
	s.mu.Lock()
	defer s.mu.Unlock()

	domainRecords := s.byDomain[domainID]
	if domainRecords == nil {
		return nil, nil, fmt.Errorf("domain %q not found", domainID)
	}
	record, ok := domainRecords[recordID]
	if !ok {
		return nil, nil, fmt.Errorf("dns record %q not found", recordID)
	}
	delete(domainRecords, recordID)
	if len(domainRecords) == 0 {
		delete(s.byDomain, domainID)
	}
	s.rebuildQuestionLocked(record.FQDN, record.RecordType)
	recordCopy := record
	return &recordCopy, s.answerSetLocked(questionFromRecord(record)), nil
}

func (s *dnsMemoryStore) RefreshQuestion(question DNSQuestion) {
	s.mu.Lock()
	defer s.mu.Unlock()
	question = normalizeQuestion(question)
	s.rebuildQuestionLocked(question.FQDN, question.RecordType)
}

func (s *dnsMemoryStore) Clear() {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.byKey = map[string][]metadata.DNSRecord{}
	s.byDomain = map[string]map[string]metadata.DNSRecord{}
}

func (s *dnsMemoryStore) upsertLocked(record metadata.DNSRecord) {
	record.FQDN = normalizeFQDN(record.FQDN)
	if s.byDomain[record.FQDN] == nil {
		s.byDomain[record.FQDN] = map[string]metadata.DNSRecord{}
	} else if existing, ok := s.byDomain[record.FQDN][record.ID]; ok {
		delete(s.byDomain[record.FQDN], record.ID)
		s.rebuildQuestionLocked(existing.FQDN, existing.RecordType)
	}
	s.byDomain[record.FQDN][record.ID] = record
	s.rebuildQuestionLocked(record.FQDN, record.RecordType)
}

func (s *dnsMemoryStore) rebuildQuestionLocked(fqdn string, recordType metadata.DNSRecordType) {
	question := normalizeQuestion(DNSQuestion{FQDN: fqdn, RecordType: recordType})
	key := dnsCacheKey(question)
	normalizedFQDN := question.FQDN
	normalizedType := question.RecordType
	var records []metadata.DNSRecord
	for _, domainRecords := range s.byDomain {
		for _, record := range domainRecords {
			if !record.Enabled {
				continue
			}
			if normalizeFQDN(record.FQDN) != normalizedFQDN {
				continue
			}
			if record.RecordType != normalizedType {
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

func (s *dnsMemoryStore) answerSetLocked(question DNSQuestion) *DNSAnswerSet {
	question = normalizeQuestion(question)
	records, ok := s.byKey[dnsCacheKey(question)]
	if !ok {
		return &DNSAnswerSet{
			Question: question,
			Found:    false,
			Records:  nil,
		}
	}
	return &DNSAnswerSet{
		Question: question,
		Found:    len(records) > 0,
		Records:  cloneDNSRecords(records),
	}
}

func cloneDNSRecords(records []metadata.DNSRecord) []metadata.DNSRecord {
	out := make([]metadata.DNSRecord, len(records))
	copy(out, records)
	return out
}

func questionFromRecord(record metadata.DNSRecord) DNSQuestion {
	return DNSQuestion{
		FQDN:       record.FQDN,
		RecordType: record.RecordType,
	}
}

func normalizeQuestion(question DNSQuestion) DNSQuestion {
	question.FQDN = normalizeFQDN(question.FQDN)
	question.RecordType = normalizeRecordType(question.RecordType)
	return question
}

func dnsCacheKey(question DNSQuestion) string {
	question = normalizeQuestion(question)
	return question.FQDN + "|" + string(question.RecordType)
}
