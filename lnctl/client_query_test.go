package lnctl

import (
	"io"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
)

func TestClientQueryDomainEntryProjection(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path != "/manage/query/domain-entry-projection" {
			t.Fatalf("unexpected path: %s", r.URL.Path)
		}
		if r.URL.Query().Get("hostname") != "app.example.com" {
			t.Fatalf("unexpected hostname query: %s", r.URL.RawQuery)
		}
		w.Header().Set("Content-Type", "application/json")
		_, _ = io.WriteString(w, `{"id":"domain:app.example.com","hostname":"app.example.com","backend_type":"l7-https","http_routes":[]}`)
	}))
	defer server.Close()

	client := NewClient(server.URL)
	got, err := client.QueryDomainEntryProjection("app.example.com")
	if err != nil {
		t.Fatalf("QueryDomainEntryProjection: %v", err)
	}
	if got.ID != "domain:app.example.com" || got.Hostname != "app.example.com" {
		t.Fatalf("unexpected projection: %+v", got)
	}
}

func TestClientQueryDNSRecords(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path != "/manage/query/dns-records" {
			t.Fatalf("unexpected path: %s", r.URL.Path)
		}
		if r.URL.Query().Get("fqdn") != "app.example.com" || r.URL.Query().Get("record_type") != "A" {
			t.Fatalf("unexpected query: %s", r.URL.RawQuery)
		}
		w.Header().Set("Content-Type", "application/json")
		_, _ = io.WriteString(w, `[{"id":"dns:app:a","fqdn":"app.example.com","record_type":"A","routing_class":"first","ttl_seconds":60,"values_json":"[\"1.2.3.4\"]","enabled":true}]`)
	}))
	defer server.Close()

	client := NewClient(server.URL)
	got, err := client.QueryDNSRecords("app.example.com", "A")
	if err != nil {
		t.Fatalf("QueryDNSRecords: %v", err)
	}
	if len(got) != 1 || got[0].ID != "dns:app:a" {
		t.Fatalf("unexpected records: %+v", got)
	}
}

func TestClientApplyPlan(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Method != http.MethodPost {
			t.Fatalf("unexpected method: %s", r.Method)
		}
		if r.URL.Path != "/manage/plan" {
			t.Fatalf("unexpected path: %s", r.URL.Path)
		}
		body, err := io.ReadAll(r.Body)
		if err != nil {
			t.Fatalf("read body: %v", err)
		}
		if !strings.Contains(string(body), `"Hostname":"app.example.com"`) {
			t.Fatalf("unexpected body: %s", string(body))
		}
		w.Header().Set("Content-Type", "application/json")
		_, _ = w.Write(body)
	}))
	defer server.Close()

	client := NewClient(server.URL)
	plan, err := client.ApplyPlan(&Plan{Hostname: "app.example.com"})
	if err != nil {
		t.Fatalf("ApplyPlan: %v", err)
	}
	if plan.Hostname != "app.example.com" {
		t.Fatalf("unexpected plan: %+v", plan)
	}
}
