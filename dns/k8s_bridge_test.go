package dns

import (
	"testing"

	"github.com/jabberwocky238/luna-edge/repository/metadata"
	"k8s.io/apimachinery/pkg/apis/meta/v1/unstructured"
)

func TestParseDNSDomainRecordState(t *testing.T) {
	state := parseDNSDomainRecordState(&unstructured.Unstructured{Object: map[string]interface{}{
		"metadata": map[string]interface{}{
			"name":       "example",
			"namespace":  "luna-edge",
			"generation": int64(3),
		},
		"spec": map[string]interface{}{
			"domain": "example.com",
			"records": []interface{}{
				map[string]interface{}{
					"name":   "@",
					"type":   "A",
					"ttl":    int64(60),
					"values": []interface{}{"1.1.1.1", "1.0.0.1"},
				},
				map[string]interface{}{
					"name":   "www",
					"type":   "CNAME",
					"values": []interface{}{"example.com"},
				},
			},
		},
	}})
	if state == nil {
		t.Fatal("expected state")
	}
	if len(state.records) != 2 {
		t.Fatalf("expected 2 records, got %d", len(state.records))
	}
	if state.records[0].FQDN != "example.com" {
		t.Fatalf("unexpected root fqdn: %s", state.records[0].FQDN)
	}
	if state.records[1].FQDN != "www.example.com" {
		t.Fatalf("unexpected child fqdn: %s", state.records[1].FQDN)
	}
	if state.records[0].Version != 3 {
		t.Fatalf("unexpected version: %d", state.records[0].Version)
	}
}

func TestEngineMergesRepositoryAndK8sRecords(t *testing.T) {
	engine := NewEngine(EngineOptions{})
	engine.RestoreRecords([]metadata.DNSRecord{{
		ID:         "repo-1",
		DomainID:   "repo",
		ZoneID:     "repo",
		FQDN:       "repo.example.com",
		RecordType: "A",
		ValuesJSON: `["2.2.2.2"]`,
		Enabled:    true,
		Version:    1,
	}})
	engine.replaceK8sRecords([]metadata.DNSRecord{{
		ID:         "k8s-1",
		DomainID:   "k8s",
		ZoneID:     "k8s",
		FQDN:       "example.com",
		RecordType: "A",
		ValuesJSON: `["1.1.1.1"]`,
		Enabled:    true,
		Version:    1,
	}})

	repoResult, err := engine.Resolve(t.Context(), "repo.example.com", "A")
	if err != nil {
		t.Fatalf("resolve repo: %v", err)
	}
	if !repoResult.Found || len(repoResult.Records) != 1 {
		t.Fatalf("expected repo record, got %+v", repoResult)
	}

	k8sResult, err := engine.Resolve(t.Context(), "example.com", "A")
	if err != nil {
		t.Fatalf("resolve k8s: %v", err)
	}
	if !k8sResult.Found || len(k8sResult.Records) != 1 {
		t.Fatalf("expected k8s record, got %+v", k8sResult)
	}
}
