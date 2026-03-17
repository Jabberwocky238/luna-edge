package manage

import (
	"context"

	"github.com/jabberwocky238/luna-edge/repository"
	"github.com/jabberwocky238/luna-edge/repository/functions"
	"github.com/jabberwocky238/luna-edge/repository/metadata"
)

type batchContextKey struct{}

type effectBatch struct {
	publish   bool
	certHosts map[string]struct{}
}

func (w *Wrapper) MarkCertificateDesired(ctx context.Context, hostname string) {
	if batch := batchFromContext(ctx); batch != nil && hostname != "" {
		batch.certHosts[hostname] = struct{}{}
	}
}

func (w *Wrapper) Batch(ctx context.Context, fn func(repo repository.Repository) error) error {
	if ctx == nil {
		ctx = context.Background()
	}
	batch := &effectBatch{certHosts: map[string]struct{}{}}
	ctx = context.WithValue(ctx, batchContextKey{}, batch)
	if err := fn(batchRepository{ctx: ctx, repo: w}); err != nil {
		return err
	}
	for host := range batch.certHosts {
		if w.notifier != nil {
			if err := w.notifier.NotifyCertificateDesired(ctx, host); err != nil {
				return err
			}
		}
	}
	if batch.publish && w.publisher != nil {
		if err := w.publisher.PublishNode(ctx, ""); err != nil {
			return err
		}
	}
	return nil
}

func batchFromContext(ctx context.Context) *effectBatch {
	if ctx == nil {
		return nil
	}
	batch, _ := ctx.Value(batchContextKey{}).(*effectBatch)
	return batch
}

type batchRepository struct {
	ctx  context.Context
	repo repository.Repository
}

func (r batchRepository) MarkCertificateDesired(ctx context.Context, hostname string) {
	if marker, ok := r.repo.(interface {
		MarkCertificateDesired(context.Context, string)
	}); ok {
		marker.MarkCertificateDesired(withBatchContext(ctx, r.ctx), hostname)
	}
}

func (r batchRepository) GetDomainEndpointByID(ctx context.Context, id string) (*metadata.DomainEndpoint, error) {
	return r.repo.GetDomainEndpointByID(withBatchContext(ctx, r.ctx), id)
}

func (r batchRepository) GetDomainEndpointByHostname(ctx context.Context, hostname string) (*metadata.DomainEndpoint, error) {
	return r.repo.GetDomainEndpointByHostname(withBatchContext(ctx, r.ctx), hostname)
}

func (r batchRepository) GetDomainEntryProjectionByDomain(ctx context.Context, domain string) (*metadata.DomainEntryProjection, error) {
	return r.repo.GetDomainEntryProjectionByDomain(withBatchContext(ctx, r.ctx), domain)
}

func (r batchRepository) ListServiceBindingsByDomainID(ctx context.Context, domainID string) ([]metadata.ServiceBackendRef, error) {
	return r.repo.ListServiceBindingsByDomainID(withBatchContext(ctx, r.ctx), domainID)
}

func (r batchRepository) ListHTTPRoutesByDomainID(ctx context.Context, domainID string) ([]metadata.HTTPRoute, error) {
	return r.repo.ListHTTPRoutesByDomainID(withBatchContext(ctx, r.ctx), domainID)
}

func (r batchRepository) GetHTTPRouteByHostname(ctx context.Context, hostname, requestPath string) (*metadata.HTTPRoute, error) {
	return r.repo.GetHTTPRouteByHostname(withBatchContext(ctx, r.ctx), hostname, requestPath)
}

func (r batchRepository) ReplaceDNSRecords(ctx context.Context, domainID string, records []metadata.DNSRecord) error {
	return r.repo.ReplaceDNSRecords(withBatchContext(ctx, r.ctx), domainID, records)
}

func (r batchRepository) ListDNSRecordsByQuestion(ctx context.Context, fqdn, recordType string) ([]metadata.DNSRecord, error) {
	return r.repo.ListDNSRecordsByQuestion(withBatchContext(ctx, r.ctx), fqdn, recordType)
}

func (r batchRepository) ListDNSRecordsByDomainID(ctx context.Context, domainID string) ([]metadata.DNSRecord, error) {
	return r.repo.ListDNSRecordsByDomainID(withBatchContext(ctx, r.ctx), domainID)
}

func (r batchRepository) GetCertificateRevision(ctx context.Context, domainID string, revision uint64) (*metadata.CertificateRevision, error) {
	return r.repo.GetCertificateRevision(withBatchContext(ctx, r.ctx), domainID, revision)
}

func (r batchRepository) GetLatestCertificateRevision(ctx context.Context, domainID string) (*metadata.CertificateRevision, error) {
	return r.repo.GetLatestCertificateRevision(withBatchContext(ctx, r.ctx), domainID)
}

func (r batchRepository) ListCertificateRevisions(ctx context.Context, domainID string) ([]metadata.CertificateRevision, error) {
	return r.repo.ListCertificateRevisions(withBatchContext(ctx, r.ctx), domainID)
}

func (r batchRepository) AppendSnapshotRecord(ctx context.Context, record *metadata.SnapshotRecord) error {
	return r.repo.AppendSnapshotRecord(withBatchContext(ctx, r.ctx), record)
}

func (r batchRepository) ListSnapshotRecordsAfter(ctx context.Context, afterID uint64) ([]metadata.SnapshotRecord, error) {
	return r.repo.ListSnapshotRecordsAfter(withBatchContext(ctx, r.ctx), afterID)
}

func (r batchRepository) DomainEndpoints() functions.GenericRepository[*metadata.DomainEndpoint] {
	return batchDomainEndpointRepo{ctx: r.ctx, repo: r.repo.DomainEndpoints()}
}

func (r batchRepository) ServiceBindingRefs() functions.GenericRepository[*metadata.ServiceBackendRef] {
	return batchServiceBackendRefRepo{ctx: r.ctx, repo: r.repo.ServiceBindingRefs()}
}

func (r batchRepository) HTTPRoutes() functions.GenericRepository[*metadata.HTTPRoute] {
	return batchHTTPRouteRepo{ctx: r.ctx, repo: r.repo.HTTPRoutes()}
}

func (r batchRepository) DNSRecords() functions.GenericRepository[*metadata.DNSRecord] {
	return batchDNSRecordRepo{ctx: r.ctx, repo: r.repo.DNSRecords()}
}

func (r batchRepository) CertificateRevisions() functions.GenericRepository[*metadata.CertificateRevision] {
	return batchCertificateRevisionRepo{ctx: r.ctx, repo: r.repo.CertificateRevisions()}
}

func (r batchRepository) SnapshotRecords() functions.GenericRepository[*metadata.SnapshotRecord] {
	return batchSnapshotRecordRepo{ctx: r.ctx, repo: r.repo.SnapshotRecords()}
}

func withBatchContext(ctx, batchCtx context.Context) context.Context {
	if batchFromContext(ctx) != nil {
		return ctx
	}
	return batchCtx
}

type batchDomainEndpointRepo struct {
	ctx  context.Context
	repo functions.GenericRepository[*metadata.DomainEndpoint]
}

func (r batchDomainEndpointRepo) ListResource(ctx context.Context, out any, orderBy string) error {
	return r.repo.ListResource(withBatchContext(ctx, r.ctx), out, orderBy)
}
func (r batchDomainEndpointRepo) GetResourceByField(ctx context.Context, out *metadata.DomainEndpoint, field string, value any) error {
	return r.repo.GetResourceByField(withBatchContext(ctx, r.ctx), out, field, value)
}
func (r batchDomainEndpointRepo) UpsertResource(ctx context.Context, model *metadata.DomainEndpoint) error {
	return r.repo.UpsertResource(withBatchContext(ctx, r.ctx), model)
}
func (r batchDomainEndpointRepo) DeleteResourceByField(ctx context.Context, model *metadata.DomainEndpoint, field string, value any) error {
	return r.repo.DeleteResourceByField(withBatchContext(ctx, r.ctx), model, field, value)
}

type batchServiceBackendRefRepo struct {
	ctx  context.Context
	repo functions.GenericRepository[*metadata.ServiceBackendRef]
}

func (r batchServiceBackendRefRepo) ListResource(ctx context.Context, out any, orderBy string) error {
	return r.repo.ListResource(withBatchContext(ctx, r.ctx), out, orderBy)
}
func (r batchServiceBackendRefRepo) GetResourceByField(ctx context.Context, out *metadata.ServiceBackendRef, field string, value any) error {
	return r.repo.GetResourceByField(withBatchContext(ctx, r.ctx), out, field, value)
}
func (r batchServiceBackendRefRepo) UpsertResource(ctx context.Context, model *metadata.ServiceBackendRef) error {
	return r.repo.UpsertResource(withBatchContext(ctx, r.ctx), model)
}
func (r batchServiceBackendRefRepo) DeleteResourceByField(ctx context.Context, model *metadata.ServiceBackendRef, field string, value any) error {
	return r.repo.DeleteResourceByField(withBatchContext(ctx, r.ctx), model, field, value)
}

type batchHTTPRouteRepo struct {
	ctx  context.Context
	repo functions.GenericRepository[*metadata.HTTPRoute]
}

func (r batchHTTPRouteRepo) ListResource(ctx context.Context, out any, orderBy string) error {
	return r.repo.ListResource(withBatchContext(ctx, r.ctx), out, orderBy)
}
func (r batchHTTPRouteRepo) GetResourceByField(ctx context.Context, out *metadata.HTTPRoute, field string, value any) error {
	return r.repo.GetResourceByField(withBatchContext(ctx, r.ctx), out, field, value)
}
func (r batchHTTPRouteRepo) UpsertResource(ctx context.Context, model *metadata.HTTPRoute) error {
	return r.repo.UpsertResource(withBatchContext(ctx, r.ctx), model)
}
func (r batchHTTPRouteRepo) DeleteResourceByField(ctx context.Context, model *metadata.HTTPRoute, field string, value any) error {
	return r.repo.DeleteResourceByField(withBatchContext(ctx, r.ctx), model, field, value)
}

type batchDNSRecordRepo struct {
	ctx  context.Context
	repo functions.GenericRepository[*metadata.DNSRecord]
}

func (r batchDNSRecordRepo) ListResource(ctx context.Context, out any, orderBy string) error {
	return r.repo.ListResource(withBatchContext(ctx, r.ctx), out, orderBy)
}
func (r batchDNSRecordRepo) GetResourceByField(ctx context.Context, out *metadata.DNSRecord, field string, value any) error {
	return r.repo.GetResourceByField(withBatchContext(ctx, r.ctx), out, field, value)
}
func (r batchDNSRecordRepo) UpsertResource(ctx context.Context, model *metadata.DNSRecord) error {
	return r.repo.UpsertResource(withBatchContext(ctx, r.ctx), model)
}
func (r batchDNSRecordRepo) DeleteResourceByField(ctx context.Context, model *metadata.DNSRecord, field string, value any) error {
	return r.repo.DeleteResourceByField(withBatchContext(ctx, r.ctx), model, field, value)
}

type batchCertificateRevisionRepo struct {
	ctx  context.Context
	repo functions.GenericRepository[*metadata.CertificateRevision]
}

func (r batchCertificateRevisionRepo) ListResource(ctx context.Context, out any, orderBy string) error {
	return r.repo.ListResource(withBatchContext(ctx, r.ctx), out, orderBy)
}
func (r batchCertificateRevisionRepo) GetResourceByField(ctx context.Context, out *metadata.CertificateRevision, field string, value any) error {
	return r.repo.GetResourceByField(withBatchContext(ctx, r.ctx), out, field, value)
}
func (r batchCertificateRevisionRepo) UpsertResource(ctx context.Context, model *metadata.CertificateRevision) error {
	return r.repo.UpsertResource(withBatchContext(ctx, r.ctx), model)
}
func (r batchCertificateRevisionRepo) DeleteResourceByField(ctx context.Context, model *metadata.CertificateRevision, field string, value any) error {
	return r.repo.DeleteResourceByField(withBatchContext(ctx, r.ctx), model, field, value)
}

type batchSnapshotRecordRepo struct {
	ctx  context.Context
	repo functions.GenericRepository[*metadata.SnapshotRecord]
}

func (r batchSnapshotRecordRepo) ListResource(ctx context.Context, out any, orderBy string) error {
	return r.repo.ListResource(withBatchContext(ctx, r.ctx), out, orderBy)
}
func (r batchSnapshotRecordRepo) GetResourceByField(ctx context.Context, out *metadata.SnapshotRecord, field string, value any) error {
	return r.repo.GetResourceByField(withBatchContext(ctx, r.ctx), out, field, value)
}
func (r batchSnapshotRecordRepo) UpsertResource(ctx context.Context, model *metadata.SnapshotRecord) error {
	return r.repo.UpsertResource(withBatchContext(ctx, r.ctx), model)
}
func (r batchSnapshotRecordRepo) DeleteResourceByField(ctx context.Context, model *metadata.SnapshotRecord, field string, value any) error {
	return r.repo.DeleteResourceByField(withBatchContext(ctx, r.ctx), model, field, value)
}
