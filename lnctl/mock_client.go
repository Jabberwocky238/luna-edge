package lnctl

import (
	"encoding/json"
	"fmt"
	"net/url"
	"strings"
	"time"

	"github.com/jabberwocky238/luna-edge/repository/metadata"
	"gorm.io/driver/sqlite"
	"gorm.io/gorm"
)

type MockClient struct {
	db      *gorm.DB
}

type MockClientInput struct {
	ID         uint      `gorm:"primaryKey"`
	Kind       string    `gorm:"column:kind;not null;index;type:text"`
	Target     string    `gorm:"column:target;not null;default:'';index;type:text"`
	RecordType string    `gorm:"column:record_type;not null;default:'';type:text"`
	Payload    string    `gorm:"column:payload;not null;default:'';type:text"`
	CreatedAt  time.Time `gorm:"column:created_at;not null;autoCreateTime"`
}

func (MockClientInput) TableName() string {
	return "lnctl_mock_inputs"
}

func NewMockClient(sqliteURL string) ClientInterface {
	dsn, err := normalizeMockSQLiteURL(sqliteURL)
	if err != nil {
		return nil
	}
	db, err := gorm.Open(sqlite.Open(dsn), &gorm.Config{})
	if err != nil {
		return nil
	}
	if err := metadata.AutoMigrate(db); err != nil {
		return nil
	}
	if err := db.AutoMigrate(&MockClientInput{}); err != nil {
		return nil
	}
	return &MockClient{db: db}
}

func (c *MockClient) Close() error {
	if c == nil || c.db == nil {
		return nil
	}
	sqlDB, err := c.db.DB()
	if err != nil {
		return err
	}
	return sqlDB.Close()
}

func (c *MockClient) Inputs() ([]MockClientInput, error) {
	if err := c.ensureReady(); err != nil {
		return nil, err
	}
	var out []MockClientInput
	if err := c.db.Order("id asc").Find(&out).Error; err != nil {
		return nil, fmt.Errorf("query mock inputs: %w", err)
	}
	return out, nil
}

func (c *MockClient) SeedDomainEntryProjection(entry *metadata.DomainEntryProjection) error {
	if err := c.ensureReady(); err != nil {
		return err
	}
	if entry == nil {
		return fmt.Errorf("entry is required")
	}
	return c.db.Transaction(func(tx *gorm.DB) error {
		endpoint := metadata.DomainEndpoint{
			Shared: metadata.Shared{
				Deleted: entry.Deleted,
			},
			Hostname:    entry.Hostname,
			NeedCert:    entry.NeedCert,
			BackendType: entry.BackendType,
		}
		if entry.BindedBackendRef != nil {
			if err := tx.Save(entry.BindedBackendRef).Error; err != nil {
				return fmt.Errorf("save binded backend ref: %w", err)
			}
			endpoint.BindedServiceID = entry.BindedBackendRef.ID
		}
		if err := tx.Save(&endpoint).Error; err != nil {
			return fmt.Errorf("save domain endpoint: %w", err)
		}
		for _, route := range entry.HTTPRoutes {
			if route.BackendRef != nil {
				if err := tx.Save(route.BackendRef).Error; err != nil {
					return fmt.Errorf("save route backend ref: %w", err)
				}
			}
			item := metadata.HTTPRoute{
				Shared:   metadata.Shared{},
				ID:       route.ID,
				Hostname: entry.Hostname,
				Path:     route.Path,
				Priority: route.Priority,
			}
			if route.BackendRef != nil {
				item.BackendRefID = route.BackendRef.ID
			}
			if err := tx.Save(&item).Error; err != nil {
				return fmt.Errorf("save http route: %w", err)
			}
		}
		return nil
	})
}

func (c *MockClient) SeedDNSRecords(records ...metadata.DNSRecord) error {
	if err := c.ensureReady(); err != nil {
		return err
	}
	for i := range records {
		if err := c.db.Save(&records[i]).Error; err != nil {
			return fmt.Errorf("save dns record %q: %w", records[i].ID, err)
		}
	}
	return nil
}

func (c *MockClient) QueryDomainEntryProjection(hostname string) (*metadata.DomainEntryProjection, error) {
	if err := c.ensureReady(); err != nil {
		return nil, err
	}
	hostname = strings.TrimSpace(hostname)
	if hostname == "" {
		return nil, fmt.Errorf("hostname is required")
	}
	if err := c.recordInput("query_domain_entry_projection", hostname, "", map[string]string{
		"hostname": hostname,
	}); err != nil {
		return nil, err
	}

	var endpoint metadata.DomainEndpoint
	if err := c.db.First(&endpoint, "hostname = ?", hostname).Error; err != nil {
		if err == gorm.ErrRecordNotFound {
			return nil, fmt.Errorf("domain entry projection not found for hostname %q", hostname)
		}
		return nil, fmt.Errorf("query domain endpoint: %w", err)
	}

	out := &metadata.DomainEntryProjection{
		Hostname:    endpoint.Hostname,
		Deleted:     endpoint.Deleted,
		NeedCert:    endpoint.NeedCert,
		BackendType: endpoint.BackendType,
	}
	if endpoint.BindedServiceID != "" {
		var backend metadata.ServiceBackendRef
		if err := c.db.First(&backend, "id = ?", endpoint.BindedServiceID).Error; err != nil {
			return nil, fmt.Errorf("query binded backend ref: %w", err)
		}
		out.BindedBackendRef = &backend
	}

	var routes []metadata.HTTPRoute
	if err := c.db.Where("hostname = ?", hostname).Order("priority desc, path asc").Find(&routes).Error; err != nil {
		return nil, fmt.Errorf("query http routes: %w", err)
	}
	out.HTTPRoutes = make([]metadata.HTTPRouteProjection, 0, len(routes))
	for _, route := range routes {
		item := metadata.HTTPRouteProjection{
			ID:       route.ID,
			Path:     route.Path,
			Priority: route.Priority,
		}
		if route.BackendRefID != "" {
			var backend metadata.ServiceBackendRef
			if err := c.db.First(&backend, "id = ?", route.BackendRefID).Error; err != nil {
				return nil, fmt.Errorf("query route backend ref: %w", err)
			}
			item.BackendRef = &backend
		}
		out.HTTPRoutes = append(out.HTTPRoutes, item)
	}
	return out, nil
}

func (c *MockClient) QueryDNSRecords(fqdn, recordType string) ([]metadata.DNSRecord, error) {
	if err := c.ensureReady(); err != nil {
		return nil, err
	}
	fqdn = strings.TrimSpace(fqdn)
	recordType = strings.TrimSpace(recordType)
	if fqdn == "" || recordType == "" {
		return nil, fmt.Errorf("fqdn and recordType are required")
	}
	if err := c.recordInput("query_dns_records", fqdn, recordType, map[string]string{
		"fqdn":        fqdn,
		"record_type": recordType,
	}); err != nil {
		return nil, err
	}
	var out []metadata.DNSRecord
	if err := c.db.Where("fqdn = ? AND record_type = ?", fqdn, recordType).Order("id asc").Find(&out).Error; err != nil {
		return nil, fmt.Errorf("query dns records: %w", err)
	}
	return out, nil
}

func (c *MockClient) ApplyPlan(plan *Plan) (*Plan, error) {
	if err := c.ensureReady(); err != nil {
		return nil, err
	}
	if plan == nil {
		return nil, fmt.Errorf("plan is required")
	}
	if err := c.recordInput("apply_plan", strings.TrimSpace(plan.Hostname), "", plan); err != nil {
		return nil, err
	}
	if err := c.db.Transaction(func(tx *gorm.DB) error {
		for _, change := range plan.DomainEndpoints {
			if err := applyDomainEndpointChange(tx, change); err != nil {
				return err
			}
		}
		for _, change := range plan.ServiceBackendRefs {
			if err := applyServiceBackendRefChange(tx, change); err != nil {
				return err
			}
		}
		for _, change := range plan.HTTPRoutes {
			if err := applyHTTPRouteChange(tx, change); err != nil {
				return err
			}
		}
		for _, change := range plan.DNSRecords {
			if err := applyDNSRecordChange(tx, change); err != nil {
				return err
			}
		}
		return nil
	}); err != nil {
		return nil, err
	}
	copyPlan := *plan
	return &copyPlan, nil
}

func (c *MockClient) recordInput(kind, target, recordType string, payload any) error {
	body, err := json.Marshal(payload)
	if err != nil {
		return fmt.Errorf("marshal mock input payload: %w", err)
	}
	input := MockClientInput{
		Kind:       kind,
		Target:     target,
		RecordType: recordType,
		Payload:    string(body),
	}
	if err := c.db.Create(&input).Error; err != nil {
		return fmt.Errorf("save mock input: %w", err)
	}
	return nil
}

func (c *MockClient) ensureReady() error {
	if c == nil {
		return fmt.Errorf("mock client is nil")
	}
	if c.db == nil {
		return fmt.Errorf("mock client database is not initialized")
	}
	return nil
}

func normalizeMockSQLiteURL(raw string) (string, error) {
	raw = strings.TrimSpace(raw)
	if raw == "" {
		return "", fmt.Errorf("sqlite url is required")
	}
	if strings.HasPrefix(raw, "file:") || raw == ":memory:" {
		return raw, nil
	}

	parsed, err := url.Parse(raw)
	if err != nil {
		return "", fmt.Errorf("parse sqlite url: %w", err)
	}
	if parsed.Scheme != "sqlite" {
		return "", fmt.Errorf("unsupported sqlite url scheme %q", parsed.Scheme)
	}

	query := parsed.Query()
	if memoryEnabled(query.Get("mode"), query.Get("memory")) {
		name := strings.TrimPrefix(parsed.Host+parsed.Path, "/")
		if name == "" {
			return "file::memory:?cache=shared", nil
		}
		cache := query.Get("cache")
		if cache == "" {
			cache = "shared"
		}
		return "file:" + name + "?mode=memory&cache=" + cache, nil
	}

	path := parsed.Host + parsed.Path
	if strings.TrimSpace(path) == "" {
		return "", fmt.Errorf("sqlite file path is required when not using memory mode")
	}
	if parsed.RawQuery != "" {
		return path + "?" + parsed.RawQuery, nil
	}
	return path, nil
}

func memoryEnabled(mode, memory string) bool {
	mode = strings.ToLower(strings.TrimSpace(mode))
	memory = strings.ToLower(strings.TrimSpace(memory))
	return mode == "memory" || memory == "1" || memory == "true" || memory == "yes"
}

func applyDomainEndpointChange(tx *gorm.DB, change DomainEndpointChange) error {
	switch change.Action {
	case PlanActionCreate, PlanActionUpdate:
		if change.Desired == nil {
			return fmt.Errorf("domain endpoint desired value is required for %s", change.Action)
		}
		if err := tx.Save(change.Desired).Error; err != nil {
			return fmt.Errorf("save domain endpoint %q: %w", change.Desired.Hostname, err)
		}
	case PlanActionDelete:
		if change.Current == nil {
			return fmt.Errorf("domain endpoint current value is required for delete")
		}
		if err := tx.Delete(&metadata.DomainEndpoint{}, "hostname = ?", change.Current.Hostname).Error; err != nil {
			return fmt.Errorf("delete domain endpoint %q: %w", change.Current.Hostname, err)
		}
	default:
		return fmt.Errorf("unsupported domain endpoint action %q", change.Action)
	}
	return nil
}

func applyServiceBackendRefChange(tx *gorm.DB, change ServiceBackendRefChange) error {
	switch change.Action {
	case PlanActionCreate, PlanActionUpdate:
		if change.Desired == nil {
			return fmt.Errorf("service backend ref desired value is required for %s", change.Action)
		}
		if err := tx.Save(change.Desired).Error; err != nil {
			return fmt.Errorf("save service backend ref %q: %w", change.Desired.ID, err)
		}
	case PlanActionDelete:
		if change.Current == nil {
			return fmt.Errorf("service backend ref current value is required for delete")
		}
		if err := tx.Delete(&metadata.ServiceBackendRef{}, "id = ?", change.Current.ID).Error; err != nil {
			return fmt.Errorf("delete service backend ref %q: %w", change.Current.ID, err)
		}
	default:
		return fmt.Errorf("unsupported service backend ref action %q", change.Action)
	}
	return nil
}

func applyHTTPRouteChange(tx *gorm.DB, change HTTPRouteChange) error {
	switch change.Action {
	case PlanActionCreate, PlanActionUpdate:
		if change.Desired == nil {
			return fmt.Errorf("http route desired value is required for %s", change.Action)
		}
		if err := tx.Save(change.Desired).Error; err != nil {
			return fmt.Errorf("save http route %q: %w", change.Desired.ID, err)
		}
	case PlanActionDelete:
		if change.Current == nil {
			return fmt.Errorf("http route current value is required for delete")
		}
		if err := tx.Delete(&metadata.HTTPRoute{}, "id = ?", change.Current.ID).Error; err != nil {
			return fmt.Errorf("delete http route %q: %w", change.Current.ID, err)
		}
	default:
		return fmt.Errorf("unsupported http route action %q", change.Action)
	}
	return nil
}

func applyDNSRecordChange(tx *gorm.DB, change DNSRecordChange) error {
	switch change.Action {
	case PlanActionCreate, PlanActionUpdate:
		if change.Desired == nil {
			return fmt.Errorf("dns record desired value is required for %s", change.Action)
		}
		if err := tx.Save(change.Desired).Error; err != nil {
			return fmt.Errorf("save dns record %q: %w", change.Desired.ID, err)
		}
	case PlanActionDelete:
		if change.Current == nil {
			return fmt.Errorf("dns record current value is required for delete")
		}
		if err := tx.Delete(&metadata.DNSRecord{}, "id = ?", change.Current.ID).Error; err != nil {
			return fmt.Errorf("delete dns record %q: %w", change.Current.ID, err)
		}
	default:
		return fmt.Errorf("unsupported dns record action %q", change.Action)
	}
	return nil
}
