package functions

import (
	"context"

	"github.com/jabberwocky238/luna-edge/repository/metadata"
)

type SpecRepository interface {
	GetDomainEndpointByID(ctx context.Context, id string) (*metadata.DomainEndpoint, error)
	GetDomainEndpointByHostname(ctx context.Context, hostname string) (*metadata.DomainEndpoint, error)

	GetDomainEntryProjectionByDomain(ctx context.Context, domain string) (*metadata.DomainEntryProjection, error)

	ListServiceBindingsByDomainID(ctx context.Context, domainID string) ([]metadata.ServiceBackendRef, error)
	ListHTTPRoutesByDomainID(ctx context.Context, domainID string) ([]metadata.HTTPRoute, error)
	ReplaceDNSRecords(ctx context.Context, domainID string, records []metadata.DNSRecord) error
	ListDNSRecordsByQuestion(ctx context.Context, fqdn, recordType string) ([]metadata.DNSRecord, error)
	ListDNSRecordsByDomainID(ctx context.Context, domainID string) ([]metadata.DNSRecord, error)
	GetCertificateRevision(ctx context.Context, domainID string, revision uint64) (*metadata.CertificateRevision, error)
	GetLatestCertificateRevision(ctx context.Context, domainID string) (*metadata.CertificateRevision, error)
	GetActiveCertificateForDomain(ctx context.Context, domain *metadata.DomainEndpoint) (*metadata.CertificateRevision, error)
	ListCertificateRevisions(ctx context.Context, domainID string) ([]metadata.CertificateRevision, error)
	AppendSnapshotRecord(ctx context.Context, record *metadata.SnapshotRecord) error
	ListSnapshotRecordsAfter(ctx context.Context, afterID uint64) ([]metadata.SnapshotRecord, error)
}

type GenericRepository[M any] interface {
	ListResource(ctx context.Context, out any, orderBy string) error
	GetResourceByField(ctx context.Context, out M, field string, value any) error
	UpsertResource(ctx context.Context, model M) error
	DeleteResourceByField(ctx context.Context, model M, field string, value any) error
}

type Repository interface {
	SpecRepository
	Begin(ctx context.Context) (Repository, error)
	Commit(ctx context.Context) error
	Rollback(ctx context.Context) error

	MarkCertificateDesired(ctx context.Context, hostname string)
	SetCertificateDesiredNotifier(func(ctx context.Context, hostname string))

	DomainEndpoints() GenericRepository[*metadata.DomainEndpoint]
	ServiceBindingRefs() GenericRepository[*metadata.ServiceBackendRef]
	HTTPRoutes() GenericRepository[*metadata.HTTPRoute]
	DNSRecords() GenericRepository[*metadata.DNSRecord]
	CertificateRevisions() GenericRepository[*metadata.CertificateRevision]
	SnapshotRecords() GenericRepository[*metadata.SnapshotRecord]
}
