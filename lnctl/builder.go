package lnctl

import (
	"fmt"
	"reflect"
	"sort"
	"strings"

	"github.com/jabberwocky238/luna-edge/repository/metadata"
)

type PlanAction string

const (
	PlanActionCreate PlanAction = "create"
	PlanActionUpdate PlanAction = "update"
	PlanActionDelete PlanAction = "delete"
)

type DNSRecordChange struct {
	Action  PlanAction
	Current *metadata.DNSRecord
	Desired *metadata.DNSRecord
}

type DomainEndpointChange struct {
	Action  PlanAction
	Current *metadata.DomainEndpoint
	Desired *metadata.DomainEndpoint
}

type ServiceBackendRefChange struct {
	Action  PlanAction
	Current *metadata.ServiceBackendRef
	Desired *metadata.ServiceBackendRef
}

type HTTPRouteChange struct {
	Action  PlanAction
	Current *metadata.HTTPRoute
	Desired *metadata.HTTPRoute
}

type Plan struct {
	Hostname           string
	DNSRecords         []DNSRecordChange
	DomainEndpoints    []DomainEndpointChange
	ServiceBackendRefs []ServiceBackendRefChange
	HTTPRoutes         []HTTPRouteChange
}

type BackendTarget struct {
	Type              metadata.ServiceBackendType
	ArbitraryEndpoint string
	ServiceNamespace  string
	ServiceName       string
	Port              uint32
}

type RouteSpec struct {
	Path    string
	Backend BackendTarget
}

type Builder struct {
	hostname string

	existingProjection *metadata.DomainEntryProjection
	existingDNSRecords []metadata.DNSRecord

	desiredBackendType metadata.BackendType
	desiredNeedCert    bool
	desiredL4Backend   *BackendTarget
	desiredRoutes      []RouteSpec
	desiredDNSRecords  []metadata.DNSRecord
}

func NewBuilder(hostname string) *Builder {
	return &Builder{hostname: strings.TrimSpace(hostname)}
}

func (b *Builder) WithExistingProjection(entry *metadata.DomainEntryProjection) *Builder {
	b.existingProjection = entry
	return b
}

func (b *Builder) WithExistingDNSRecords(records ...metadata.DNSRecord) *Builder {
	b.existingDNSRecords = append([]metadata.DNSRecord(nil), records...)
	return b
}

func (b *Builder) AsL4TLSPassthrough(backend BackendTarget) *Builder {
	b.desiredBackendType = metadata.BackendTypeL4TLSPassthrough
	b.desiredNeedCert = false
	b.desiredL4Backend = &backend
	b.desiredRoutes = nil
	return b
}

func (b *Builder) AsL4TLSTermination(backend BackendTarget) *Builder {
	b.desiredBackendType = metadata.BackendTypeL4TLSTermination
	b.desiredNeedCert = true
	b.desiredL4Backend = &backend
	b.desiredRoutes = nil
	return b
}

func (b *Builder) AsL7HTTP() *Builder {
	b.desiredBackendType = metadata.BackendTypeL7HTTP
	b.desiredNeedCert = false
	b.desiredL4Backend = nil
	return b
}

func (b *Builder) AsL7HTTPS() *Builder {
	b.desiredBackendType = metadata.BackendTypeL7HTTPS
	b.desiredNeedCert = true
	b.desiredL4Backend = nil
	return b
}

func (b *Builder) AsL7HTTPBoth() *Builder {
	b.desiredBackendType = metadata.BackendTypeL7HTTPBoth
	b.desiredNeedCert = true
	b.desiredL4Backend = nil
	return b
}

func (b *Builder) Route(path string, backend BackendTarget) *Builder {
	b.desiredRoutes = append(b.desiredRoutes, RouteSpec{
		Path:    normalizeBuilderPath(path),
		Backend: backend,
	})
	return b
}

func (b *Builder) WantDNS(record metadata.DNSRecord) *Builder {
	b.desiredDNSRecords = append(b.desiredDNSRecords, record)
	return b
}

func (b *Builder) Build() (*Plan, error) {
	if b.hostname == "" {
		return nil, fmt.Errorf("hostname is required")
	}
	if b.desiredBackendType == "" {
		return nil, fmt.Errorf("backend type is required")
	}
	if isL4BackendType(b.desiredBackendType) && b.desiredL4Backend == nil {
		return nil, fmt.Errorf("l4 backend is required")
	}
	if isL7BackendType(b.desiredBackendType) && len(b.desiredRoutes) == 0 {
		return nil, fmt.Errorf("at least one l7 route is required")
	}

	plan := &Plan{Hostname: b.hostname}
	desiredDomain := b.buildDesiredDomainEndpoint()
	plan.DomainEndpoints = append(plan.DomainEndpoints, diffDomainEndpoint(b.currentDomainEndpoint(), desiredDomain)...)

	if isL4BackendType(b.desiredBackendType) {
		desiredBackend := b.buildBackendRef("l4", *b.desiredL4Backend)
		plan.ServiceBackendRefs = append(plan.ServiceBackendRefs, diffServiceBackendRef(b.currentL4BackendRef(), &desiredBackend)...)
		plan.HTTPRoutes = append(plan.HTTPRoutes, diffHTTPRoutes(b.currentHTTPRoutes(), nil)...)
		for _, backend := range b.currentL7BackendRefs() {
			plan.ServiceBackendRefs = append(plan.ServiceBackendRefs, diffServiceBackendRef(&backend, nil)...)
		}
	} else {
		desiredRoutes, desiredBackends := b.buildDesiredL7Resources(desiredDomain.ID)
		plan.HTTPRoutes = append(plan.HTTPRoutes, diffHTTPRoutes(b.currentHTTPRoutes(), desiredRoutes)...)
		plan.ServiceBackendRefs = append(plan.ServiceBackendRefs, diffServiceBackendRefs(b.currentL7BackendRefs(), desiredBackends)...)
		if current := b.currentL4BackendRef(); current != nil {
			plan.ServiceBackendRefs = append(plan.ServiceBackendRefs, diffServiceBackendRef(current, nil)...)
		}
	}

	plan.DNSRecords = append(plan.DNSRecords, diffDNSRecords(b.existingDNSRecords, b.buildDesiredDNSRecords())...)
	return plan, nil
}

func (b *Builder) buildDesiredDomainEndpoint() *metadata.DomainEndpoint {
	id := "domain:" + b.hostname
	if current := b.currentDomainEndpoint(); current != nil && current.ID != "" {
		id = current.ID
	}
	out := &metadata.DomainEndpoint{
		ID:          id,
		Hostname:    b.hostname,
		NeedCert:    b.desiredNeedCert,
		BackendType: b.desiredBackendType,
	}
	if b.desiredL4Backend != nil {
		out.BindedServiceID = b.backendRefID("l4")
	}
	return out
}

func (b *Builder) buildDesiredL7Resources(domainID string) ([]metadata.HTTPRoute, []metadata.ServiceBackendRef) {
	routes := make([]metadata.HTTPRoute, 0, len(b.desiredRoutes))
	backends := make([]metadata.ServiceBackendRef, 0, len(b.desiredRoutes))
	for _, route := range b.desiredRoutes {
		refID := b.backendRefID(route.Path)
		backends = append(backends, b.buildBackendRef(route.Path, route.Backend))
		routes = append(routes, metadata.HTTPRoute{
			ID:               b.routeID(route.Path),
			DomainEndpointID: domainID,
			Path:             route.Path,
			Priority:         int32(len(route.Path)),
			BackendRefID:     refID,
		})
	}
	return routes, backends
}

func (b *Builder) buildBackendRef(key string, target BackendTarget) metadata.ServiceBackendRef {
	typ := target.Type
	if typ == "" {
		typ = metadata.ServiceBackendTypeSVC
	}
	return metadata.ServiceBackendRef{
		ID:                b.backendRefID(key),
		Type:              typ,
		ArbitraryEndpoint: strings.TrimSpace(target.ArbitraryEndpoint),
		ServiceNamespace:  strings.TrimSpace(target.ServiceNamespace),
		ServiceName:       strings.TrimSpace(target.ServiceName),
		Port:              target.Port,
	}
}

func (b *Builder) buildDesiredDNSRecords() []metadata.DNSRecord {
	out := make([]metadata.DNSRecord, 0, len(b.desiredDNSRecords))
	for _, item := range b.desiredDNSRecords {
		record := item
		if strings.TrimSpace(record.ID) == "" {
			record.ID = defaultDNSRecordID(record)
		}
		out = append(out, record)
	}
	return out
}

func (b *Builder) currentDomainEndpoint() *metadata.DomainEndpoint {
	if b.existingProjection == nil {
		return nil
	}
	return &metadata.DomainEndpoint{
		Shared:          metadata.Shared{Deleted: b.existingProjection.Deleted},
		ID:              b.existingProjection.ID,
		Hostname:        b.existingProjection.Hostname,
		NeedCert:        b.existingProjection.Cert != nil,
		BackendType:     b.existingProjection.BackendType,
		BindedServiceID: currentBindedServiceID(b.existingProjection),
	}
}

func (b *Builder) currentL4BackendRef() *metadata.ServiceBackendRef {
	if b.existingProjection == nil {
		return nil
	}
	return b.existingProjection.BindedBackendRef
}

func (b *Builder) currentL7BackendRefs() []metadata.ServiceBackendRef {
	if b.existingProjection == nil {
		return nil
	}
	seen := map[string]metadata.ServiceBackendRef{}
	for _, route := range b.existingProjection.HTTPRoutes {
		if route.BackendRef == nil || route.BackendRef.ID == "" {
			continue
		}
		seen[route.BackendRef.ID] = *route.BackendRef
	}
	out := make([]metadata.ServiceBackendRef, 0, len(seen))
	for _, item := range seen {
		out = append(out, item)
	}
	sort.Slice(out, func(i, j int) bool { return out[i].ID < out[j].ID })
	return out
}

func (b *Builder) currentHTTPRoutes() []metadata.HTTPRoute {
	if b.existingProjection == nil {
		return nil
	}
	out := make([]metadata.HTTPRoute, 0, len(b.existingProjection.HTTPRoutes))
	for _, route := range b.existingProjection.HTTPRoutes {
		item := metadata.HTTPRoute{
			ID:               route.ID,
			DomainEndpointID: b.existingProjection.ID,
			Path:             route.Path,
			Priority:         route.Priority,
		}
		if route.BackendRef != nil {
			item.BackendRefID = route.BackendRef.ID
		}
		out = append(out, item)
	}
	return out
}

func (b *Builder) backendRefID(key string) string {
	return "backend:" + b.hostname + ":" + sanitizeBuilderIDPart(key)
}

func (b *Builder) routeID(path string) string {
	return "route:" + b.hostname + ":" + sanitizeBuilderIDPart(path)
}

func diffDomainEndpoint(current, desired *metadata.DomainEndpoint) []DomainEndpointChange {
	if current == nil && desired == nil {
		return nil
	}
	if current == nil {
		return []DomainEndpointChange{{Action: PlanActionCreate, Desired: desired}}
	}
	if desired == nil {
		return []DomainEndpointChange{{Action: PlanActionDelete, Current: current}}
	}
	if reflect.DeepEqual(*current, *desired) {
		return nil
	}
	return []DomainEndpointChange{{Action: PlanActionUpdate, Current: current, Desired: desired}}
}

func diffServiceBackendRefs(current, desired []metadata.ServiceBackendRef) []ServiceBackendRefChange {
	currentMap := make(map[string]metadata.ServiceBackendRef, len(current))
	desiredMap := make(map[string]metadata.ServiceBackendRef, len(desired))
	for _, item := range current {
		currentMap[item.ID] = item
	}
	for _, item := range desired {
		desiredMap[item.ID] = item
	}

	var out []ServiceBackendRefChange
	for id, item := range desiredMap {
		currentItem, ok := currentMap[id]
		if !ok {
			copyItem := item
			out = append(out, ServiceBackendRefChange{Action: PlanActionCreate, Desired: &copyItem})
			continue
		}
		if reflect.DeepEqual(currentItem, item) {
			continue
		}
		currentCopy := currentItem
		desiredCopy := item
		out = append(out, ServiceBackendRefChange{Action: PlanActionUpdate, Current: &currentCopy, Desired: &desiredCopy})
	}
	for id, item := range currentMap {
		if _, ok := desiredMap[id]; ok {
			continue
		}
		copyItem := item
		out = append(out, ServiceBackendRefChange{Action: PlanActionDelete, Current: &copyItem})
	}
	sort.Slice(out, func(i, j int) bool { return serviceBackendRefChangeID(out[i]) < serviceBackendRefChangeID(out[j]) })
	return out
}

func diffServiceBackendRef(current, desired *metadata.ServiceBackendRef) []ServiceBackendRefChange {
	if current == nil && desired == nil {
		return nil
	}
	if current == nil {
		return []ServiceBackendRefChange{{Action: PlanActionCreate, Desired: desired}}
	}
	if desired == nil {
		return []ServiceBackendRefChange{{Action: PlanActionDelete, Current: current}}
	}
	if reflect.DeepEqual(*current, *desired) {
		return nil
	}
	return []ServiceBackendRefChange{{Action: PlanActionUpdate, Current: current, Desired: desired}}
}

func diffHTTPRoutes(current, desired []metadata.HTTPRoute) []HTTPRouteChange {
	currentMap := make(map[string]metadata.HTTPRoute, len(current))
	desiredMap := make(map[string]metadata.HTTPRoute, len(desired))
	for _, item := range current {
		currentMap[item.ID] = item
	}
	for _, item := range desired {
		desiredMap[item.ID] = item
	}

	var out []HTTPRouteChange
	for id, item := range desiredMap {
		currentItem, ok := currentMap[id]
		if !ok {
			copyItem := item
			out = append(out, HTTPRouteChange{Action: PlanActionCreate, Desired: &copyItem})
			continue
		}
		if reflect.DeepEqual(currentItem, item) {
			continue
		}
		currentCopy := currentItem
		desiredCopy := item
		out = append(out, HTTPRouteChange{Action: PlanActionUpdate, Current: &currentCopy, Desired: &desiredCopy})
	}
	for id, item := range currentMap {
		if _, ok := desiredMap[id]; ok {
			continue
		}
		copyItem := item
		out = append(out, HTTPRouteChange{Action: PlanActionDelete, Current: &copyItem})
	}
	sort.Slice(out, func(i, j int) bool { return httpRouteChangeID(out[i]) < httpRouteChangeID(out[j]) })
	return out
}

func diffDNSRecords(current, desired []metadata.DNSRecord) []DNSRecordChange {
	currentMap := make(map[string]metadata.DNSRecord, len(current))
	desiredMap := make(map[string]metadata.DNSRecord, len(desired))
	for _, item := range current {
		currentMap[item.ID] = item
	}
	for _, item := range desired {
		desiredMap[item.ID] = item
	}

	var out []DNSRecordChange
	for id, item := range desiredMap {
		currentItem, ok := currentMap[id]
		if !ok {
			copyItem := item
			out = append(out, DNSRecordChange{Action: PlanActionCreate, Desired: &copyItem})
			continue
		}
		if reflect.DeepEqual(currentItem, item) {
			continue
		}
		currentCopy := currentItem
		desiredCopy := item
		out = append(out, DNSRecordChange{Action: PlanActionUpdate, Current: &currentCopy, Desired: &desiredCopy})
	}
	for id, item := range currentMap {
		if _, ok := desiredMap[id]; ok {
			continue
		}
		copyItem := item
		out = append(out, DNSRecordChange{Action: PlanActionDelete, Current: &copyItem})
	}
	sort.Slice(out, func(i, j int) bool { return dnsRecordChangeID(out[i]) < dnsRecordChangeID(out[j]) })
	return out
}

func normalizeBuilderPath(path string) string {
	path = strings.TrimSpace(path)
	if path == "" || path[0] != '/' {
		return "/"
	}
	return path
}

func sanitizeBuilderIDPart(value string) string {
	value = strings.TrimSpace(strings.ToLower(value))
	replacer := strings.NewReplacer("/", "_", ".", "_", ":", "_", "*", "wildcard", " ", "_")
	value = replacer.Replace(value)
	value = strings.Trim(value, "_")
	if value == "" {
		return "root"
	}
	return value
}

func defaultDNSRecordID(record metadata.DNSRecord) string {
	parts := []string{
		"dns",
		sanitizeBuilderIDPart(record.FQDN),
		sanitizeBuilderIDPart(string(record.RecordType)),
	}
	if strings.TrimSpace(record.RoutingKey) != "" {
		parts = append(parts, sanitizeBuilderIDPart(record.RoutingKey))
	}
	return strings.Join(parts, ":")
}

func currentBindedServiceID(entry *metadata.DomainEntryProjection) string {
	if entry == nil || entry.BindedBackendRef == nil {
		return ""
	}
	return entry.BindedBackendRef.ID
}

func isL4BackendType(kind metadata.BackendType) bool {
	return kind == metadata.BackendTypeL4TLSPassthrough || kind == metadata.BackendTypeL4TLSTermination
}

func isL7BackendType(kind metadata.BackendType) bool {
	return kind == metadata.BackendTypeL7HTTP || kind == metadata.BackendTypeL7HTTPS || kind == metadata.BackendTypeL7HTTPBoth
}

func serviceBackendRefChangeID(change ServiceBackendRefChange) string {
	if change.Desired != nil {
		return change.Desired.ID
	}
	if change.Current != nil {
		return change.Current.ID
	}
	return ""
}

func httpRouteChangeID(change HTTPRouteChange) string {
	if change.Desired != nil {
		return change.Desired.ID
	}
	if change.Current != nil {
		return change.Current.ID
	}
	return ""
}

func dnsRecordChangeID(change DNSRecordChange) string {
	if change.Desired != nil {
		return change.Desired.ID
	}
	if change.Current != nil {
		return change.Current.ID
	}
	return ""
}
