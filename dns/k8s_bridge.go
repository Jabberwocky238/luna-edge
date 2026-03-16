package dns

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

type K8sBridge struct {
	namespace     string
	dynamicClient dynamic.Interface
	factory       dynamicinformer.DynamicSharedInformerFactory
	stopCh        chan struct{}
	onChange      func([]metadata.DNSRecord)

	mu      sync.RWMutex
	records map[string]*k8sDNSDomainRecordState
}

type k8sDNSDomainRecordState struct {
	key     string
	records []metadata.DNSRecord
}

func NewK8sBridge(namespace string) (*K8sBridge, error) {
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
	return NewK8sBridgeWithClient(namespace, client), nil
}

func NewK8sBridgeWithClient(namespace string, dynamicClient dynamic.Interface) *K8sBridge {
	if namespace == "" {
		namespace = enginepkg.POD_NAMESPACE
	}
	if namespace == "" {
		namespace = "default"
	}
	bridge := &K8sBridge{
		namespace:     namespace,
		dynamicClient: dynamicClient,
		stopCh:        make(chan struct{}),
		records:       make(map[string]*k8sDNSDomainRecordState),
	}
	bridge.ensureInformer()
	return bridge
}

func (b *K8sBridge) SetOnChange(onChange func([]metadata.DNSRecord)) {
	if b == nil {
		return
	}
	b.mu.Lock()
	b.onChange = onChange
	records := b.flattenRecordsLocked()
	b.mu.Unlock()
	if onChange != nil {
		onChange(records)
	}
}

func (b *K8sBridge) LoadInitial(ctx context.Context) error {
	if b == nil || b.dynamicClient == nil {
		return nil
	}
	list, err := b.dynamicClient.Resource(dnsDomainRecordGVR).Namespace(b.namespace).List(ctx, metav1.ListOptions{})
	if err != nil {
		return fmt.Errorf("list dnsdomainrecords: %w", err)
	}
	b.mu.Lock()
	for i := range list.Items {
		item := list.Items[i]
		if state := parseDNSDomainRecordState(item.DeepCopy()); state != nil {
			b.records[state.key] = state
		}
	}
	records := b.flattenRecordsLocked()
	onChange := b.onChange
	b.mu.Unlock()
	if onChange != nil {
		onChange(records)
	}
	return nil
}

func (b *K8sBridge) Listen() {
	if b == nil || b.factory == nil {
		return
	}
	b.factory.Start(b.stopCh)
}

func (b *K8sBridge) Stop() error {
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

func (b *K8sBridge) ensureInformer() {
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
		UpdateFunc: func(_, newObj interface{}) { b.storeObject(newObj) },
		DeleteFunc: b.deleteObject,
	})
}

func (b *K8sBridge) storeObject(obj interface{}) {
	state := parseDNSDomainRecordState(asUnstructured(obj))
	if state == nil {
		return
	}
	b.mu.Lock()
	b.records[state.key] = state
	records := b.flattenRecordsLocked()
	onChange := b.onChange
	b.mu.Unlock()
	if onChange != nil {
		onChange(records)
	}
}

func (b *K8sBridge) deleteObject(obj interface{}) {
	deleteByNamespaceName(obj, func(namespace, name string) {
		b.mu.Lock()
		delete(b.records, namespace+"/"+name)
		records := b.flattenRecordsLocked()
		onChange := b.onChange
		b.mu.Unlock()
		if onChange != nil {
			onChange(records)
		}
	})
}

func (b *K8sBridge) flattenRecordsLocked() []metadata.DNSRecord {
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

func parseDNSDomainRecordState(obj *unstructured.Unstructured) *k8sDNSDomainRecordState {
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
		recordType, _, _ := unstructured.NestedString(item, "type")
		recordType = normalizeRecordType(recordType)
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
			ZoneID:       domainID,
			DomainID:     domainID,
			FQDN:         buildRecordFQDN(domain, label),
			RecordType:   recordType,
			RoutingClass: "simple",
			TTLSeconds:   ttl,
			ValuesJSON:   string(valuesJSON),
			Enabled:      true,
			Version:      generation,
		})
	}
	return &k8sDNSDomainRecordState{
		key:     namespace + "/" + name,
		records: records,
	}
}

func buildRecordFQDN(domain, label string) string {
	domain = strings.TrimSuffix(strings.TrimSpace(domain), ".")
	label = strings.TrimSpace(label)
	switch label {
	case "", "@":
		return domain
	default:
		return strings.TrimSuffix(label, ".") + "." + domain
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

func parseUint64OrDefault(value string, fallback uint64) uint64 {
	value = strings.TrimSpace(value)
	if value == "" {
		return fallback
	}
	parsed, err := strconv.ParseUint(value, 10, 64)
	if err != nil {
		return fallback
	}
	return parsed
}

func asUnstructured(obj interface{}) *unstructured.Unstructured {
	value, ok := obj.(*unstructured.Unstructured)
	if ok {
		return value
	}
	return nil
}

func deleteByNamespaceName(obj interface{}, deleteFn func(namespace, name string)) {
	if tombstone, ok := obj.(cache.DeletedFinalStateUnknown); ok {
		obj = tombstone.Obj
	}
	switch value := obj.(type) {
	case *unstructured.Unstructured:
		if value != nil {
			deleteFn(value.GetNamespace(), value.GetName())
		}
	case metav1.Object:
		deleteFn(value.GetNamespace(), value.GetName())
	}
}
