package dns

import (
	"context"
	"encoding/json"
	"net"
	"os"
	"path/filepath"
	"testing"
	"time"

	"github.com/jabberwocky238/luna-edge/repository/metadata"
	mdns "github.com/miekg/dns"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/apis/meta/v1/unstructured"
	"k8s.io/apimachinery/pkg/runtime"
	"k8s.io/apimachinery/pkg/runtime/schema"
	dynamicfake "k8s.io/client-go/dynamic/fake"
	"k8s.io/client-go/tools/cache"
	"sigs.k8s.io/yaml"
)

type dnsResponseRecorder struct {
	msg        *mdns.Msg
	remoteAddr net.Addr
}

func (r *dnsResponseRecorder) LocalAddr() net.Addr          { return &net.UDPAddr{IP: net.IPv4zero, Port: 53} }
func (r *dnsResponseRecorder) RemoteAddr() net.Addr         { return r.remoteAddr }
func (r *dnsResponseRecorder) WriteMsg(msg *mdns.Msg) error { r.msg = msg.Copy(); return nil }
func (r *dnsResponseRecorder) Write([]byte) (int, error)    { return 0, nil }
func (r *dnsResponseRecorder) Close() error                 { return nil }
func (r *dnsResponseRecorder) TsigStatus() error            { return nil }
func (r *dnsResponseRecorder) TsigTimersOnly(bool)          {}
func (r *dnsResponseRecorder) Hijack()                      {}

func TestEngineTracksDnsDomainRecordChangesWithFakeKube(t *testing.T) {
	ctx := t.Context()
	client := dynamicfake.NewSimpleDynamicClientWithCustomListKinds(
		runtime.NewScheme(),
		map[schema.GroupVersionResource]string{
			dnsDomainRecordGVR: "DnsDomainRecordList",
		},
	)
	crd := loadMockDNSCRD(t)
	created, err := client.Resource(dnsDomainRecordGVR).Namespace("luna-edge").Create(ctx, crd, metav1.CreateOptions{})
	if err != nil {
		t.Fatalf("create dnsdomainrecord before load initial: %v", err)
	}
	engine := NewEngine(EngineOptions{})
	bridge := NewK8sBridgeWithClient("luna-edge", client)
	bridge.SetOnChange(func(records []metadata.DNSRecord) {
		engine.replaceK8sRecords(records)
	})
	if err := bridge.LoadInitial(ctx); err != nil {
		t.Fatalf("load initial bridge state: %v", err)
	}
	bridge.mu.RLock()
	initialBridgeRecords := bridge.flattenRecordsLocked()
	bridge.mu.RUnlock()
	if len(initialBridgeRecords) == 0 {
		t.Fatal("expected initial bridge records after loading existing dnsdomainrecord")
	}
	engine.k8sBridge = bridge
	bridge.Listen(ctx)
	if !cache.WaitForCacheSync(ctx.Done(), bridge.factory.ForResource(dnsDomainRecordGVR).Informer().HasSynced) {
		t.Fatal("wait for dns informer cache sync")
	}
	t.Cleanup(func() {
		if err := bridge.Stop(); err != nil {
			t.Fatalf("stop bridge: %v", err)
		}
	})

	waitForRecordValue(t, engine, "example.com", "A", "1.1.1.1")
	assertDNSReply(t, engine, "example.com.", mdns.TypeA, []string{"1.1.1.1"})

	updated := created.DeepCopy()
	records, found, err := unstructured.NestedSlice(updated.Object, "spec", "records")
	if err != nil || !found || len(records) == 0 {
		t.Fatalf("read records from updated object: found=%v err=%v", found, err)
	}
	first, ok := records[0].(map[string]interface{})
	if !ok {
		t.Fatalf("unexpected first record type: %T", records[0])
	}
	first["values"] = []interface{}{"1.0.0.1"}
	records[0] = first
	if err := unstructured.SetNestedSlice(updated.Object, records, "spec", "records"); err != nil {
		t.Fatalf("write updated records back to object: %v", err)
	}
	updated.SetGeneration(2)
	if _, err := client.Resource(dnsDomainRecordGVR).Namespace("luna-edge").Update(ctx, updated, metav1.UpdateOptions{}); err != nil {
		t.Fatalf("update dnsdomainrecord: %v", err)
	}

	waitForRecordValue(t, engine, "example.com", "A", "1.0.0.1")
	assertDNSReply(t, engine, "example.com.", mdns.TypeA, []string{"1.0.0.1"})

	if err := client.Resource(dnsDomainRecordGVR).Namespace("luna-edge").Delete(ctx, crd.GetName(), metav1.DeleteOptions{}); err != nil {
		t.Fatalf("delete dnsdomainrecord: %v", err)
	}
	waitForNoRecord(t, engine, "example.com", "A")
	assertNXDomainReply(t, engine, "example.com.", mdns.TypeA)
}

func loadMockDNSCRD(t *testing.T) *unstructured.Unstructured {
	t.Helper()
	path := filepath.Join("..", "deploy", "luna-edge-mock-dns.yaml")
	raw, err := os.ReadFile(path)
	if err != nil {
		t.Fatalf("read mock dns yaml: %v", err)
	}
	jsonBytes, err := yaml.YAMLToJSON(raw)
	if err != nil {
		t.Fatalf("yaml to json: %v", err)
	}
	var obj map[string]any
	if err := json.Unmarshal(jsonBytes, &obj); err != nil {
		t.Fatalf("unmarshal crd object: %v", err)
	}
	return &unstructured.Unstructured{Object: obj}
}

func waitForRecordValue(t *testing.T, engine *Engine, hostname, recordType, expected string) {
	t.Helper()
	waitForCondition(t, func() bool {
		result, err := engine.Lookup(context.Background(), DNSQuestion{
			FQDN:       hostname,
			RecordType: metadata.DNSRecordType(recordType),
		})
		if err != nil || result == nil || !result.Found || len(result.Records) == 0 {
			return false
		}
		values := splitValues(result.Records[0].ValuesJSON)
		return len(values) == 1 && values[0] == expected
	})
}

func waitForNoRecord(t *testing.T, engine *Engine, hostname, recordType string) {
	t.Helper()
	waitForCondition(t, func() bool {
		result, err := engine.Lookup(context.Background(), DNSQuestion{
			FQDN:       hostname,
			RecordType: metadata.DNSRecordType(recordType),
		})
		return err == nil && result != nil && !result.Found
	})
}

func waitForCondition(t *testing.T, check func() bool) {
	t.Helper()
	deadline := time.Now().Add(3 * time.Second)
	for time.Now().Before(deadline) {
		if check() {
			return
		}
		time.Sleep(20 * time.Millisecond)
	}
	t.Fatal("condition not satisfied before timeout")
}

func assertDNSReply(t *testing.T, engine *Engine, fqdn string, qtype uint16, expected []string) {
	t.Helper()
	req := new(mdns.Msg)
	req.SetQuestion(fqdn, qtype)
	recorder := &dnsResponseRecorder{remoteAddr: &net.UDPAddr{IP: net.ParseIP("203.0.113.10"), Port: 53000}}
	engine.serveDNS(recorder, req)
	if recorder.msg == nil {
		t.Fatal("expected dns response")
	}
	if recorder.msg.Rcode != mdns.RcodeSuccess {
		t.Fatalf("unexpected rcode: %d", recorder.msg.Rcode)
	}
	if len(recorder.msg.Answer) != len(expected) {
		t.Fatalf("unexpected answer count: got %d want %d", len(recorder.msg.Answer), len(expected))
	}
	for i, answer := range recorder.msg.Answer {
		a, ok := answer.(*mdns.A)
		if !ok {
			t.Fatalf("expected A record answer, got %T", answer)
		}
		if a.A.String() != expected[i] {
			t.Fatalf("unexpected A value at %d: got %s want %s", i, a.A.String(), expected[i])
		}
	}
}

func assertNXDomainReply(t *testing.T, engine *Engine, fqdn string, qtype uint16) {
	t.Helper()
	req := new(mdns.Msg)
	req.SetQuestion(fqdn, qtype)
	recorder := &dnsResponseRecorder{remoteAddr: &net.UDPAddr{IP: net.ParseIP("203.0.113.10"), Port: 53000}}
	engine.serveDNS(recorder, req)
	if recorder.msg == nil {
		t.Fatal("expected dns response")
	}
	if recorder.msg.Rcode != mdns.RcodeNameError {
		t.Fatalf("unexpected rcode after delete: got %d want %d", recorder.msg.Rcode, mdns.RcodeNameError)
	}
	if len(recorder.msg.Answer) != 0 {
		t.Fatalf("expected no answers after delete, got %d", len(recorder.msg.Answer))
	}
}
