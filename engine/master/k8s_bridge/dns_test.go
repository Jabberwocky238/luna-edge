package k8s_bridge

import (
	"context"
	"testing"
	"time"

	"github.com/jabberwocky238/luna-edge/engine"
	"github.com/jabberwocky238/luna-edge/engine/master/manage"
	"github.com/jabberwocky238/luna-edge/repository"
	"github.com/jabberwocky238/luna-edge/repository/connection"
	"github.com/jabberwocky238/luna-edge/repository/metadata"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/apis/meta/v1/unstructured"
	"k8s.io/apimachinery/pkg/runtime"
	"k8s.io/apimachinery/pkg/runtime/schema"
	dynamicfake "k8s.io/client-go/dynamic/fake"
	"k8s.io/client-go/tools/cache"
)

type fakePublisher struct {
	count int
}

func (f *fakePublisher) PublishSnapshot(_ context.Context, _ *engine.Snapshot) error {
	return nil
}

func (f *fakePublisher) PublishNode(_ context.Context, _ string) error {
	f.count++
	return nil
}

func TestDNSBridgeSyncsDnsDomainRecordIntoRepository(t *testing.T) {
	factory, err := repository.NewFactory(connection.Config{
		Driver:      connection.DriverSQLite,
		Path:        t.TempDir() + "/master.db",
		AutoMigrate: true,
	})
	if err != nil {
		t.Fatalf("new factory: %v", err)
	}
	defer func() { _ = factory.Close() }()

	client := dynamicfake.NewSimpleDynamicClientWithCustomListKinds(
		runtime.NewScheme(),
		map[schema.GroupVersionResource]string{
			dnsDomainRecordGVR: "DnsDomainRecordList",
		},
	)
	pub := &fakePublisher{}
	repo := manage.NewWrapper(factory.Repository(), pub, nil)
	bridge := NewDNSBridgeWithClient("luna-edge", client, repo)

	crd := &unstructured.Unstructured{Object: map[string]interface{}{
		"apiVersion": "luna-edge.io/v1alpha1",
		"kind":       "DnsDomainRecord",
		"metadata": map[string]interface{}{
			"name":       "example",
			"namespace":  "luna-edge",
			"generation": int64(1),
		},
		"spec": map[string]interface{}{
			"domain": "example.com",
			"records": []interface{}{
				map[string]interface{}{
					"name":   "@",
					"type":   "A",
					"ttl":    int64(60),
					"values": []interface{}{"1.1.1.1"},
				},
			},
		},
	}}
	if _, err := client.Resource(dnsDomainRecordGVR).Namespace("luna-edge").Create(context.Background(), crd, metav1.CreateOptions{}); err != nil {
		t.Fatalf("create dnsdomainrecord: %v", err)
	}

	if err := bridge.LoadInitial(context.Background()); err != nil {
		t.Fatalf("load initial: %v", err)
	}

	records, err := factory.Repository().ListDNSRecordsByQuestion(context.Background(), "example.com", "A")
	if err != nil {
		t.Fatalf("list dns records: %v", err)
	}
	if len(records) != 1 || records[0].ValuesJSON != `["1.1.1.1"]` {
		t.Fatalf("unexpected synced records: %+v", records)
	}
	if pub.count != 1 {
		t.Fatalf("expected one publish after initial load, got %d", pub.count)
	}
}

func TestDNSBridgeTracksAddUpdateDelete(t *testing.T) {
	factory, err := repository.NewFactory(connection.Config{
		Driver:      connection.DriverSQLite,
		Path:        t.TempDir() + "/master.db",
		AutoMigrate: true,
	})
	if err != nil {
		t.Fatalf("new factory: %v", err)
	}
	defer func() { _ = factory.Close() }()

	client := dynamicfake.NewSimpleDynamicClientWithCustomListKinds(
		runtime.NewScheme(),
		map[schema.GroupVersionResource]string{
			dnsDomainRecordGVR: "DnsDomainRecordList",
		},
	)
	pub := &fakePublisher{}
	repo := manage.NewWrapper(factory.Repository(), pub, nil)
	bridge := NewDNSBridgeWithClient("luna-edge", client, repo)
	bridge.Listen()
	if !cache.WaitForCacheSync(context.Background().Done(), bridge.factory.ForResource(dnsDomainRecordGVR).Informer().HasSynced) {
		t.Fatal("wait for informer sync")
	}
	defer func() { _ = bridge.Stop() }()

	crd := &unstructured.Unstructured{Object: map[string]interface{}{
		"apiVersion": "luna-edge.io/v1alpha1",
		"kind":       "DnsDomainRecord",
		"metadata": map[string]interface{}{
			"name":       "example",
			"namespace":  "luna-edge",
			"generation": int64(1),
		},
		"spec": map[string]interface{}{
			"domain": "example.com",
			"records": []interface{}{
				map[string]interface{}{
					"name":   "@",
					"type":   "A",
					"values": []interface{}{"1.1.1.1"},
				},
			},
		},
	}}
	created, err := client.Resource(dnsDomainRecordGVR).Namespace("luna-edge").Create(context.Background(), crd, metav1.CreateOptions{})
	if err != nil {
		t.Fatalf("create dnsdomainrecord: %v", err)
	}
	waitForDNSValue(t, factory.Repository(), "example.com", "A", `["1.1.1.1"]`)

	updated := created.DeepCopy()
	records, _, _ := unstructured.NestedSlice(updated.Object, "spec", "records")
	item := records[0].(map[string]interface{})
	item["values"] = []interface{}{"1.0.0.1"}
	records[0] = item
	updated.SetGeneration(2)
	if err := unstructured.SetNestedSlice(updated.Object, records, "spec", "records"); err != nil {
		t.Fatalf("set nested records: %v", err)
	}
	if _, err := client.Resource(dnsDomainRecordGVR).Namespace("luna-edge").Update(context.Background(), updated, metav1.UpdateOptions{}); err != nil {
		t.Fatalf("update dnsdomainrecord: %v", err)
	}
	waitForDNSValue(t, factory.Repository(), "example.com", "A", `["1.0.0.1"]`)

	if err := client.Resource(dnsDomainRecordGVR).Namespace("luna-edge").Delete(context.Background(), "example", metav1.DeleteOptions{}); err != nil {
		t.Fatalf("delete dnsdomainrecord: %v", err)
	}
	waitForNoDNSValue(t, factory.Repository(), "example.com", "A")

	if pub.count < 3 {
		t.Fatalf("expected publishes for create/update/delete, got %d", pub.count)
	}
}

func waitForDNSValue(t *testing.T, repo repository.Repository, fqdn, recordType, expected string) {
	t.Helper()
	waitForCondition(t, func() bool {
		records, err := repo.ListDNSRecordsByQuestion(context.Background(), fqdn, recordType)
		return err == nil && len(records) == 1 && records[0].ValuesJSON == expected
	})
}

func waitForNoDNSValue(t *testing.T, repo repository.Repository, fqdn, recordType string) {
	t.Helper()
	waitForCondition(t, func() bool {
		records, err := repo.ListDNSRecordsByQuestion(context.Background(), fqdn, recordType)
		return err == nil && len(records) == 0
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
	if state.records[0].RoutingClass != metadata.RoutingClassFirst {
		t.Fatalf("unexpected routing class: %+v", state.records[0])
	}
}
