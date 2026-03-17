package k8s_bridge

import (
	"context"
	"encoding/json"
	"fmt"
	"sort"
	"strconv"
	"strings"
	"sync"
	"time"

	enginepkg "github.com/jabberwocky238/luna-edge/engine"
	"github.com/jabberwocky238/luna-edge/repository"
	"github.com/jabberwocky238/luna-edge/repository/metadata"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/apis/meta/v1/unstructured"
	"k8s.io/apimachinery/pkg/runtime/schema"
	"k8s.io/client-go/dynamic"
	dynamicinformer "k8s.io/client-go/dynamic/dynamicinformer"
	"k8s.io/client-go/rest"
	"k8s.io/client-go/tools/cache"
)

var dnsDomainRecordGVR = schema.GroupVersionResource{
	Group:    "luna-edge.io",
	Version:  "v1alpha1",
	Resource: "dnsdomainrecords",
}

type publisher interface {
	PublishNode(ctx context.Context, nodeID string) error
}

type DNSBridge struct {
	namespace     string
	dynamicClient dynamic.Interface
	factory       dynamicinformer.DynamicSharedInformerFactory
	stopCh        chan struct{}
	repo          repository.Repository
	publisher     publisher

	mu      sync.RWMutex
	records map[string]*dnsDomainRecordState
}

type dnsDomainRecordState struct {
	key     string
	records []metadata.DNSRecord
}

func NewDNSBridge(namespace string, repo repository.Repository, pub publisher) (*DNSBridge, error) {
	if namespace == "" {
		namespace = enginepkg.POD_NAMESPACE
	}
	if namespace == "" {
		namespace = "default"
	}

	cfg, err := rest.InClusterConfig()
	if err != nil {
		return nil, fmt.Errorf("create in-cluster k8s config: %w", err)
	}
	client, err := dynamic.NewForConfig(cfg)
	if err != nil {
		return nil, fmt.Errorf("create dynamic k8s client: %w", err)
	}
	return NewDNSBridgeWithClient(namespace, client, repo, pub), nil
}

func NewDNSBridgeWithClient(namespace string, dynamicClient dynamic.Interface, repo repository.Repository, pub publisher) *DNSBridge {
	if namespace == "" {
		namespace = enginepkg.POD_NAMESPACE
	}
	if namespace == "" {
		namespace = "default"
	}
	bridge := &DNSBridge{
		namespace:     namespace,
		dynamicClient: dynamicClient,
		stopCh:        make(chan struct{}),
		repo:          repo,
		publisher:     pub,
		records:       make(map[string]*dnsDomainRecordState),
	}
	bridge.ensureInformer()
	return bridge
}

func (b *DNSBridge) LoadInitial(ctx context.Context) error {
	if b == nil || b.dynamicClient == nil {
		return nil
	}
	list, err := b.dynamicClient.Resource(dnsDomainRecordGVR).Namespace(b.namespace).List(ctx, metav1.ListOptions{})
	if err != nil {
		return fmt.Errorf("list dnsdomainrecords: %w", err)
	}
	b.mu.Lock()
	b.records = make(map[string]*dnsDomainRecordState, len(list.Items))
	for i := range list.Items {
		item := list.Items[i]
		if state := parseDNSDomainRecordState(item.DeepCopy()); state != nil {
			b.records[state.key] = state
		}
	}
	records := b.flattenRecordsLocked()
	b.mu.Unlock()
	return b.syncRecords(ctx, records)
}

func (b *DNSBridge) Listen() {
	if b == nil || b.factory == nil {
		return
	}
	b.factory.Start(b.stopCh)
}

func (b *DNSBridge) Stop() error {
	if b == nil {
		return nil
	}
	select {
	case <-b.stopCh:
		return nil
	default:
		close(b.stopCh)
		return nil
	}
}

func (b *DNSBridge) ensureInformer() {
	if b == nil || b.dynamicClient == nil || b.factory != nil {
		return
	}
	b.factory = dynamicinformer.NewFilteredDynamicSharedInformerFactory(
		b.dynamicClient,
		30*time.Second,
		b.namespace,
		nil,
	)
	b.factory.ForResource(dnsDomainRecordGVR).Informer().AddEventHandler(cache.ResourceEventHandlerFuncs{
		AddFunc:    b.storeObject,
		UpdateFunc: b.updateObject,
		DeleteFunc: b.deleteObject,
	})
}

func (b *DNSBridge) storeObject(obj interface{}) {
	state := parseDNSDomainRecordState(asUnstructured(obj))
	if state == nil {
		return
	}
	b.mu.Lock()
	b.records[state.key] = state
	records := b.flattenRecordsLocked()
	b.mu.Unlock()
	_ = b.syncRecords(context.Background(), records)
}

func (b *DNSBridge) updateObject(_old, obj interface{}) {
	b.storeObject(obj)
}

func (b *DNSBridge) deleteObject(obj interface{}) {
	deleteByNamespaceName(obj, func(namespace, name string) {
		b.mu.Lock()
		delete(b.records, namespace+"/"+name)
		records := b.flattenRecordsLocked()
		b.mu.Unlock()
		_ = b.syncRecords(context.Background(), records)
	})
}

func (b *DNSBridge) syncRecords(ctx context.Context, records []metadata.DNSRecord) error {
	if b == nil || b.repo == nil {
		return nil
	}
	var existing []metadata.DNSRecord
	if err := b.repo.DNSRecords().ListResource(ctx, &existing, "id asc"); err != nil {
		return err
	}
	existingByID := make(map[string]metadata.DNSRecord, len(existing))
	for i := range existing {
		if strings.HasPrefix(existing[i].ID, "k8s:") {
			existingByID[existing[i].ID] = existing[i]
		}
	}
	nextIDs := make(map[string]struct{}, len(records))
	for i := range records {
		nextIDs[records[i].ID] = struct{}{}
		if err := b.repo.DNSRecords().UpsertResource(ctx, &records[i]); err != nil {
			return err
		}
	}
	for id := range existingByID {
		if _, ok := nextIDs[id]; ok {
			continue
		}
		if err := b.repo.DNSRecords().DeleteResourceByField(ctx, &metadata.DNSRecord{}, "id", id); err != nil {
			return err
		}
	}
	if b.publisher != nil {
		return b.publisher.PublishNode(ctx, "")
	}
	return nil
}

func (b *DNSBridge) flattenRecordsLocked() []metadata.DNSRecord {
	out := make([]metadata.DNSRecord, 0)
	keys := make([]string, 0, len(b.records))
	for key := range b.records {
		keys = append(keys, key)
	}
	sort.Strings(keys)
	for _, key := range keys {
		out = append(out, cloneDNSRecords(b.records[key].records)...)
	}
	return out
}

func parseDNSDomainRecordState(obj *unstructured.Unstructured) *dnsDomainRecordState {
	if obj == nil {
		return nil
	}
	namespace := obj.GetNamespace()
	name := obj.GetName()
	if namespace == "" || name == "" {
		return nil
	}
	domain, _, _ := unstructured.NestedString(obj.Object, "spec", "domain")
	domain = strings.TrimSpace(domain)
	if domain == "" {
		return nil
	}
	generation := uint64(obj.GetGeneration())
	if generation == 0 {
		generation = parseUint64OrDefault(obj.GetResourceVersion(), 1)
	}
	if generation == 0 {
		generation = 1
	}
	items, _, _ := unstructured.NestedSlice(obj.Object, "spec", "records")
	records := make([]metadata.DNSRecord, 0, len(items))
	domainID := "k8s:" + namespace + ":" + name
	for idx, raw := range items {
		item, ok := raw.(map[string]interface{})
		if !ok {
			continue
		}
		recordTypeValue, _, _ := unstructured.NestedString(item, "type")
		recordType := normalizeRecordType(metadata.DNSRecordType(recordTypeValue))
		if recordType == "" {
			continue
		}
		values, _, _ := unstructured.NestedStringSlice(item, "values")
		values = compactStrings(values)
		if len(values) == 0 {
			continue
		}
		label, _, _ := unstructured.NestedString(item, "name")
		ttl64, foundTTL, _ := unstructured.NestedInt64(item, "ttl")
		ttl := uint32(60)
		if foundTTL && ttl64 >= 0 {
			ttl = uint32(ttl64)
		}
		valuesJSON, _ := json.Marshal(values)
		records = append(records, metadata.DNSRecord{
			ID:           fmt.Sprintf("%s:%d", domainID, idx),
			FQDN:         joinDNSLabel(label, domain),
			RecordType:   recordType,
			RoutingClass: metadata.RoutingClassFirst,
			TTLSeconds:   ttl,
			ValuesJSON:   string(valuesJSON),
			Enabled:      true,
			Shared: metadata.Shared{
				Deleted: false,
			},
		})
	}
	return &dnsDomainRecordState{
		key:     namespace + "/" + name,
		records: records,
	}
}

func joinDNSLabel(label, domain string) string {
	label = strings.TrimSpace(label)
	domain = strings.Trim(strings.TrimSpace(domain), ".")
	switch label {
	case "", "@":
		return domain
	default:
		return strings.Trim(label, ".") + "." + domain
	}
}

func normalizeRecordType(recordType metadata.DNSRecordType) metadata.DNSRecordType {
	switch strings.ToUpper(strings.TrimSpace(string(recordType))) {
	case "A":
		return metadata.DNSTypeA
	case "AAAA":
		return metadata.DNSTypeAAAA
	case "CNAME":
		return metadata.DNSTypeCNAME
	case "TXT":
		return metadata.DNSTypeTXT
	case "MX":
		return metadata.DNSTypeMX
	case "NS":
		return metadata.DNSTypeNS
	case "SRV":
		return metadata.DNSTypeSRV
	case "CAA":
		return metadata.DNSTypeCAA
	default:
		return ""
	}
}

func compactStrings(values []string) []string {
	out := make([]string, 0, len(values))
	for _, value := range values {
		value = strings.TrimSpace(value)
		if value == "" {
			continue
		}
		out = append(out, value)
	}
	return out
}

func cloneDNSRecords(records []metadata.DNSRecord) []metadata.DNSRecord {
	if len(records) == 0 {
		return nil
	}
	out := make([]metadata.DNSRecord, len(records))
	copy(out, records)
	return out
}

func parseUint64OrDefault(value string, fallback uint64) uint64 {
	n, err := strconv.ParseUint(strings.TrimSpace(value), 10, 64)
	if err != nil {
		return fallback
	}
	return n
}

func asUnstructured(obj interface{}) *unstructured.Unstructured {
	value, ok := obj.(*unstructured.Unstructured)
	if ok {
		return value
	}
	return nil
}

func deleteByNamespaceName(obj interface{}, deleter func(namespace, name string)) {
	switch value := obj.(type) {
	case metav1.Object:
		deleter(value.GetNamespace(), value.GetName())
	case cache.DeletedFinalStateUnknown:
		accessor, ok := value.Obj.(metav1.Object)
		if ok && accessor != nil {
			deleter(accessor.GetNamespace(), accessor.GetName())
		}
	}
}
