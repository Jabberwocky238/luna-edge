package manage

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"time"

	enginepkg "github.com/jabberwocky238/luna-edge/engine"
	"github.com/jabberwocky238/luna-edge/repository/functions"
	"github.com/jabberwocky238/luna-edge/repository/metadata"
	"gorm.io/gorm"
)

type certificateDesiredNotifier interface {
	NotifyCertificateDesired(ctx context.Context, fqdn string) error
}

// Wrapper 承担 manage 层的统一 CRUD 和自动副作用。
// 它复用底层 gorm generic repository，只在少数写操作外面包一层。
type Wrapper struct {
	raw       functions.Repository
	publisher enginepkg.Publisher
	notifier  certificateDesiredNotifier
}

func NewWrapper(repo functions.Repository, publisher enginepkg.Publisher, notifier certificateDesiredNotifier) *Wrapper {
	return &Wrapper{
		raw:       repo,
		publisher: publisher,
		notifier:  notifier,
	}
}

func (w *Wrapper) DomainEndpoints() functions.GenericRepository[*metadata.DomainEndpoint] {
	return domainEndpointRepository{wrapper: w, raw: w.raw.DomainEndpoints()}
}

func (w *Wrapper) ServiceBindingRefs() functions.GenericRepository[*metadata.ServiceBackendRef] {
	return serviceBackendRefRepository{wrapper: w, raw: w.raw.ServiceBindingRefs()}
}

func (w *Wrapper) HTTPRoutes() functions.GenericRepository[*metadata.HTTPRoute] {
	return httpRouteRepository{wrapper: w, raw: w.raw.HTTPRoutes()}
}

func (w *Wrapper) DNSRecords() functions.GenericRepository[*metadata.DNSRecord] {
	return dnsRecordRepository{wrapper: w, raw: w.raw.DNSRecords()}
}

func (w *Wrapper) CertificateRevisions() functions.GenericRepository[*metadata.CertificateRevision] {
	return certificateRevisionRepository{wrapper: w, raw: w.raw.CertificateRevisions()}
}

func (w *Wrapper) SnapshotRecords() functions.GenericRepository[*metadata.SnapshotRecord] {
	return w.raw.SnapshotRecords()
}

func (w *Wrapper) GetDomainEndpointByID(ctx context.Context, id string) (*metadata.DomainEndpoint, error) {
	return w.raw.GetDomainEndpointByID(ctx, id)
}

func (w *Wrapper) GetDomainEndpointByHostname(ctx context.Context, hostname string) (*metadata.DomainEndpoint, error) {
	return w.raw.GetDomainEndpointByHostname(ctx, hostname)
}

func (w *Wrapper) GetDomainEntryProjectionByDomain(ctx context.Context, domain string) (*metadata.DomainEntryProjection, error) {
	return w.raw.GetDomainEntryProjectionByDomain(ctx, domain)
}

func (w *Wrapper) ListServiceBindingsByDomainID(ctx context.Context, domainID string) ([]metadata.ServiceBackendRef, error) {
	return w.raw.ListServiceBindingsByDomainID(ctx, domainID)
}

func (w *Wrapper) ListHTTPRoutesByDomainID(ctx context.Context, domainID string) ([]metadata.HTTPRoute, error) {
	return w.raw.ListHTTPRoutesByDomainID(ctx, domainID)
}

func (w *Wrapper) ReplaceDNSRecords(ctx context.Context, domainID string, records []metadata.DNSRecord) error {
	return w.raw.ReplaceDNSRecords(ctx, domainID, records)
}

func (w *Wrapper) ListDNSRecordsByQuestion(ctx context.Context, fqdn, recordType string) ([]metadata.DNSRecord, error) {
	return w.raw.ListDNSRecordsByQuestion(ctx, fqdn, recordType)
}

func (w *Wrapper) ListDNSRecordsByDomainID(ctx context.Context, domainID string) ([]metadata.DNSRecord, error) {
	return w.raw.ListDNSRecordsByDomainID(ctx, domainID)
}

func (w *Wrapper) GetCertificateRevision(ctx context.Context, domainID string, revision uint64) (*metadata.CertificateRevision, error) {
	return w.raw.GetCertificateRevision(ctx, domainID, revision)
}

func (w *Wrapper) GetLatestCertificateRevision(ctx context.Context, domainID string) (*metadata.CertificateRevision, error) {
	return w.raw.GetLatestCertificateRevision(ctx, domainID)
}

func (w *Wrapper) GetActiveCertificateForDomain(ctx context.Context, domain *metadata.DomainEndpoint) (*metadata.CertificateRevision, error) {
	return w.raw.GetActiveCertificateForDomain(ctx, domain)
}

func (w *Wrapper) ListCertificateRevisions(ctx context.Context, domainID string) ([]metadata.CertificateRevision, error) {
	return w.raw.ListCertificateRevisions(ctx, domainID)
}

func (w *Wrapper) AppendSnapshotRecord(ctx context.Context, record *metadata.SnapshotRecord) error {
	return w.raw.AppendSnapshotRecord(ctx, record)
}

func (w *Wrapper) ListSnapshotRecordsAfter(ctx context.Context, afterID uint64) ([]metadata.SnapshotRecord, error) {
	return w.raw.ListSnapshotRecordsAfter(ctx, afterID)
}

func (w *Wrapper) List(ctx context.Context, resource string) (any, error) {
	desc, err := lookupDescriptor(resource)
	if err != nil {
		return nil, err
	}
	slicePtr := newSlicePtr(desc.newModel)
	if err := w.listByResource(ctx, resource, slicePtr, desc.idField+" asc"); err != nil {
		return nil, err
	}
	return derefValue(slicePtr), nil
}

func (w *Wrapper) Get(ctx context.Context, resource, id string) (any, error) {
	desc, err := lookupDescriptor(resource)
	if err != nil {
		return nil, err
	}
	model := desc.newModel()
	if err := w.getByResource(ctx, resource, model, desc.idField, id); err != nil {
		return nil, err
	}
	return model, nil
}

func (w *Wrapper) UpsertJSON(ctx context.Context, resource string, body []byte) (any, error) {
	desc, err := lookupDescriptor(resource)
	if err != nil {
		return nil, err
	}
	model := desc.newModel()
	if err := json.Unmarshal(body, model); err != nil {
		return nil, err
	}
	if err := w.upsertByResource(ctx, resource, model); err != nil {
		return nil, err
	}
	return model, nil
}

func (w *Wrapper) Delete(ctx context.Context, resource, id string) error {
	desc, err := lookupDescriptor(resource)
	if err != nil {
		return err
	}
	model := desc.newModel()
	return w.deleteByResource(ctx, resource, model, desc.idField, id)
}

func (w *Wrapper) listByResource(ctx context.Context, resource string, out any, orderBy string) error {
	switch resource {
	case "domain_endpoints":
		return w.raw.DomainEndpoints().ListResource(ctx, out, orderBy)
	case "service_backend_refs":
		return w.raw.ServiceBindingRefs().ListResource(ctx, out, orderBy)
	case "http_routes":
		return w.raw.HTTPRoutes().ListResource(ctx, out, orderBy)
	case "dns_records":
		return w.raw.DNSRecords().ListResource(ctx, out, orderBy)
	case "certificate_revisions":
		return w.raw.CertificateRevisions().ListResource(ctx, out, orderBy)
	case "snapshot_records":
		return w.raw.SnapshotRecords().ListResource(ctx, out, orderBy)
	default:
		return fmt.Errorf("unsupported resource %q", resource)
	}
}

func (w *Wrapper) getByResource(ctx context.Context, resource string, model any, field string, value any) error {
	switch typed := model.(type) {
	case *metadata.DomainEndpoint:
		return w.raw.DomainEndpoints().GetResourceByField(ctx, typed, field, value)
	case *metadata.ServiceBackendRef:
		return w.raw.ServiceBindingRefs().GetResourceByField(ctx, typed, field, value)
	case *metadata.HTTPRoute:
		return w.raw.HTTPRoutes().GetResourceByField(ctx, typed, field, value)
	case *metadata.DNSRecord:
		return w.raw.DNSRecords().GetResourceByField(ctx, typed, field, value)
	case *metadata.CertificateRevision:
		return w.raw.CertificateRevisions().GetResourceByField(ctx, typed, field, value)
	case *metadata.SnapshotRecord:
		return w.raw.SnapshotRecords().GetResourceByField(ctx, typed, field, value)
	default:
		return fmt.Errorf("unsupported resource %q", resource)
	}
}

func (w *Wrapper) upsertByResource(ctx context.Context, resource string, model any) error {
	switch typed := model.(type) {
	case *metadata.DomainEndpoint:
		return w.upsertDomainEndpoint(ctx, typed)
	case *metadata.ServiceBackendRef:
		return w.upsertServiceBackendRef(ctx, typed)
	case *metadata.HTTPRoute:
		return w.upsertHTTPRoute(ctx, typed)
	case *metadata.DNSRecord:
		return w.upsertDNSRecord(ctx, typed)
	case *metadata.CertificateRevision:
		return w.upsertCertificateRevision(ctx, typed)
	case *metadata.SnapshotRecord:
		return w.raw.SnapshotRecords().UpsertResource(ctx, typed)
	default:
		return fmt.Errorf("unsupported resource %q", resource)
	}
}

func (w *Wrapper) deleteByResource(ctx context.Context, resource string, model any, field string, value any) error {
	switch typed := model.(type) {
	case *metadata.DomainEndpoint:
		return w.deleteDomainEndpoint(ctx, typed, field, value)
	case *metadata.ServiceBackendRef:
		return w.deleteServiceBackendRef(ctx, typed, field, value)
	case *metadata.HTTPRoute:
		return w.deleteHTTPRoute(ctx, typed, field, value)
	case *metadata.DNSRecord:
		return w.deleteDNSRecord(ctx, typed, field, value)
	case *metadata.CertificateRevision:
		return w.deleteCertificateRevision(ctx, typed, field, value)
	case *metadata.SnapshotRecord:
		return w.raw.SnapshotRecords().DeleteResourceByField(ctx, typed, field, value)
	default:
		return fmt.Errorf("unsupported resource %q", resource)
	}
}

func (w *Wrapper) loadExistingDomainEndpoint(ctx context.Context, domain *metadata.DomainEndpoint) (*metadata.DomainEndpoint, error) {
	if domain == nil || domain.ID == "" {
		return nil, nil
	}
	current := &metadata.DomainEndpoint{}
	err := w.raw.DomainEndpoints().GetResourceByField(ctx, current, "id", domain.ID)
	if err != nil {
		if errors.Is(err, gorm.ErrRecordNotFound) {
			return nil, nil
		}
		return nil, err
	}
	return current, nil
}

func (w *Wrapper) maybeNotifyNeedCertChange(ctx context.Context, previous, current *metadata.DomainEndpoint) error {
	if w.notifier == nil || current == nil || !current.NeedCert || current.Hostname == "" {
		return nil
	}
	if previous == nil || !previous.NeedCert || previous.Hostname != current.Hostname {
		if batch := batchFromContext(ctx); batch != nil {
			batch.certHosts[current.Hostname] = struct{}{}
			return nil
		}
		return w.notifier.NotifyCertificateDesired(ctx, current.Hostname)
	}
	return nil
}

func (w *Wrapper) upsertDomainEndpoint(ctx context.Context, domain *metadata.DomainEndpoint) error {
	previous, err := w.loadExistingDomainEndpoint(ctx, domain)
	if err != nil {
		return err
	}
	if err := w.raw.DomainEndpoints().UpsertResource(ctx, domain); err != nil {
		return err
	}
	if err := publishDomain(ctx, w, domain.ID); err != nil {
		return err
	}
	return w.maybeNotifyNeedCertChange(ctx, previous, domain)
}

func (w *Wrapper) deleteDomainEndpoint(ctx context.Context, model *metadata.DomainEndpoint, field string, value any) error {
	current := &metadata.DomainEndpoint{}
	if err := w.raw.DomainEndpoints().GetResourceByField(ctx, current, field, value); err != nil {
		return err
	}
	if err := w.raw.DomainEndpoints().DeleteResourceByField(ctx, model, field, value); err != nil {
		return err
	}
	return publishDeleteForDomain(ctx, w, current.ID, nil, current.Hostname)
}

func (w *Wrapper) upsertServiceBackendRef(ctx context.Context, backend *metadata.ServiceBackendRef) error {
	if err := w.raw.ServiceBindingRefs().UpsertResource(ctx, backend); err != nil {
		return err
	}
	return nil
}

func (w *Wrapper) deleteServiceBackendRef(ctx context.Context, model *metadata.ServiceBackendRef, field string, value any) error {
	current := &metadata.ServiceBackendRef{}
	if err := w.raw.ServiceBindingRefs().GetResourceByField(ctx, current, field, value); err != nil {
		return err
	}
	if err := w.raw.ServiceBindingRefs().DeleteResourceByField(ctx, model, field, value); err != nil {
		return err
	}
	return nil
}

func (w *Wrapper) upsertHTTPRoute(ctx context.Context, route *metadata.HTTPRoute) error {
	if err := w.raw.HTTPRoutes().UpsertResource(ctx, route); err != nil {
		return err
	}
	return publishRoute(ctx, w, route.DomainEndpointID, "")
}

func (w *Wrapper) deleteHTTPRoute(ctx context.Context, model *metadata.HTTPRoute, field string, value any) error {
	current := &metadata.HTTPRoute{}
	if err := w.raw.HTTPRoutes().GetResourceByField(ctx, current, field, value); err != nil {
		return err
	}
	if err := w.raw.HTTPRoutes().DeleteResourceByField(ctx, model, field, value); err != nil {
		return err
	}
	return publishDomain(ctx, w, current.DomainEndpointID)
}

func (w *Wrapper) upsertDNSRecord(ctx context.Context, record *metadata.DNSRecord) error {
	if err := w.raw.DNSRecords().UpsertResource(ctx, record); err != nil {
		return err
	}
	return publishAllModel(ctx, w, record)
}

func (w *Wrapper) deleteDNSRecord(ctx context.Context, model *metadata.DNSRecord, field string, value any) error {
	current := &metadata.DNSRecord{}
	if err := w.raw.DNSRecords().GetResourceByField(ctx, current, field, value); err != nil {
		return err
	}
	if err := w.raw.DNSRecords().DeleteResourceByField(ctx, model, field, value); err != nil {
		return err
	}
	if w.publisher == nil {
		return nil
	}
	return w.publisher.PublishChangeLog(ctx, &enginepkg.ChangeNotification{
		NodeID:    enginepkg.POD_NAME,
		CreatedAt: time.Now().UTC(),
		DNSRecord: &metadata.DNSRecord{
			Shared: metadata.Shared{
				Deleted: true,
			},
			ID:         current.ID,
			FQDN:       current.FQDN,
			RecordType: current.RecordType,
		},
	})
}

func (w *Wrapper) upsertCertificateRevision(ctx context.Context, cert *metadata.CertificateRevision) error {
	if err := w.raw.CertificateRevisions().UpsertResource(ctx, cert); err != nil {
		return err
	}
	return publishCertificate(ctx, w, cert.DomainEndpointID, cert.Revision)
}

func (w *Wrapper) deleteCertificateRevision(ctx context.Context, model *metadata.CertificateRevision, field string, value any) error {
	current := &metadata.CertificateRevision{}
	if err := w.raw.CertificateRevisions().GetResourceByField(ctx, current, field, value); err != nil {
		return err
	}
	if err := w.raw.CertificateRevisions().DeleteResourceByField(ctx, model, field, value); err != nil {
		return err
	}
	return publishDeleteForDomain(ctx, w, current.DomainEndpointID, nil, current.ID)
}

type domainEndpointRepository struct {
	wrapper *Wrapper
	raw     functions.GenericRepository[*metadata.DomainEndpoint]
}

func (r domainEndpointRepository) ListResource(ctx context.Context, out any, orderBy string) error {
	return r.raw.ListResource(ctx, out, orderBy)
}

func (r domainEndpointRepository) GetResourceByField(ctx context.Context, out *metadata.DomainEndpoint, field string, value any) error {
	return r.raw.GetResourceByField(ctx, out, field, value)
}

func (r domainEndpointRepository) UpsertResource(ctx context.Context, model *metadata.DomainEndpoint) error {
	return r.wrapper.upsertDomainEndpoint(ctx, model)
}

func (r domainEndpointRepository) DeleteResourceByField(ctx context.Context, model *metadata.DomainEndpoint, field string, value any) error {
	return r.wrapper.deleteDomainEndpoint(ctx, model, field, value)
}

type serviceBackendRefRepository struct {
	wrapper *Wrapper
	raw     functions.GenericRepository[*metadata.ServiceBackendRef]
}

func (r serviceBackendRefRepository) ListResource(ctx context.Context, out any, orderBy string) error {
	return r.raw.ListResource(ctx, out, orderBy)
}

func (r serviceBackendRefRepository) GetResourceByField(ctx context.Context, out *metadata.ServiceBackendRef, field string, value any) error {
	return r.raw.GetResourceByField(ctx, out, field, value)
}

func (r serviceBackendRefRepository) UpsertResource(ctx context.Context, model *metadata.ServiceBackendRef) error {
	return r.wrapper.upsertServiceBackendRef(ctx, model)
}

func (r serviceBackendRefRepository) DeleteResourceByField(ctx context.Context, model *metadata.ServiceBackendRef, field string, value any) error {
	return r.wrapper.deleteServiceBackendRef(ctx, model, field, value)
}

type httpRouteRepository struct {
	wrapper *Wrapper
	raw     functions.GenericRepository[*metadata.HTTPRoute]
}

func (r httpRouteRepository) ListResource(ctx context.Context, out any, orderBy string) error {
	return r.raw.ListResource(ctx, out, orderBy)
}

func (r httpRouteRepository) GetResourceByField(ctx context.Context, out *metadata.HTTPRoute, field string, value any) error {
	return r.raw.GetResourceByField(ctx, out, field, value)
}

func (r httpRouteRepository) UpsertResource(ctx context.Context, model *metadata.HTTPRoute) error {
	return r.wrapper.upsertHTTPRoute(ctx, model)
}

func (r httpRouteRepository) DeleteResourceByField(ctx context.Context, model *metadata.HTTPRoute, field string, value any) error {
	return r.wrapper.deleteHTTPRoute(ctx, model, field, value)
}

type dnsRecordRepository struct {
	wrapper *Wrapper
	raw     functions.GenericRepository[*metadata.DNSRecord]
}

func (r dnsRecordRepository) ListResource(ctx context.Context, out any, orderBy string) error {
	return r.raw.ListResource(ctx, out, orderBy)
}

func (r dnsRecordRepository) GetResourceByField(ctx context.Context, out *metadata.DNSRecord, field string, value any) error {
	return r.raw.GetResourceByField(ctx, out, field, value)
}

func (r dnsRecordRepository) UpsertResource(ctx context.Context, model *metadata.DNSRecord) error {
	return r.wrapper.upsertDNSRecord(ctx, model)
}

func (r dnsRecordRepository) DeleteResourceByField(ctx context.Context, model *metadata.DNSRecord, field string, value any) error {
	return r.wrapper.deleteDNSRecord(ctx, model, field, value)
}

type certificateRevisionRepository struct {
	wrapper *Wrapper
	raw     functions.GenericRepository[*metadata.CertificateRevision]
}

func (r certificateRevisionRepository) ListResource(ctx context.Context, out any, orderBy string) error {
	return r.raw.ListResource(ctx, out, orderBy)
}

func (r certificateRevisionRepository) GetResourceByField(ctx context.Context, out *metadata.CertificateRevision, field string, value any) error {
	return r.raw.GetResourceByField(ctx, out, field, value)
}

func (r certificateRevisionRepository) UpsertResource(ctx context.Context, model *metadata.CertificateRevision) error {
	return r.wrapper.upsertCertificateRevision(ctx, model)
}

func (r certificateRevisionRepository) DeleteResourceByField(ctx context.Context, model *metadata.CertificateRevision, field string, value any) error {
	return r.wrapper.deleteCertificateRevision(ctx, model, field, value)
}
