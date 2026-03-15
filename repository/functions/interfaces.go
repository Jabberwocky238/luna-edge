package functions

import (
	"context"

	"github.com/jabberwocky238/luna-edge/repository/metadata"
)

type SpecRepository interface {
	GetZoneByName(ctx context.Context, name string) (*metadata.Zone, error)
	GetDomainEndpointByHostname(ctx context.Context, hostname string) (*metadata.DomainEndpoint, error)
	GetDomainEndpointStatus(ctx context.Context, domainID string) (*metadata.DomainEndpointStatus, error)
	GetServiceBindingByDomainID(ctx context.Context, domainID string) (*metadata.ServiceBinding, error)
	GetServiceBindingByHostname(ctx context.Context, hostname string) (*metadata.ServiceBinding, error)
	ReplaceDNSProjection(ctx context.Context, projection *metadata.DNSProjection, records []metadata.DNSRecord) error
	ListDNSRecordsByQuestion(ctx context.Context, fqdn, recordType string) ([]metadata.DNSRecord, error)
	ListDNSRecordsByDomainID(ctx context.Context, domainID string) ([]metadata.DNSRecord, error)
	GetCertificateRevision(ctx context.Context, domainID string, revision uint64) (*metadata.CertificateRevision, error)
	GetLatestCertificateRevision(ctx context.Context, domainID string) (*metadata.CertificateRevision, error)
	ListCertificateRevisions(ctx context.Context, domainID string) ([]metadata.CertificateRevision, error)
	ListACMEChallengesByOrderID(ctx context.Context, orderID string) ([]metadata.ACMEChallenge, error)
	ListAttachmentsByNodeID(ctx context.Context, nodeID string) ([]metadata.Attachment, error)
	ListAttachmentsByDomainID(ctx context.Context, domainID string) ([]metadata.Attachment, error)
}

type GenericRepository[M any] interface {
	ListResource(ctx context.Context, out any, orderBy string) error
	GetResourceByField(ctx context.Context, out M, field string, value any) error
	UpsertResource(ctx context.Context, model M) error
	DeleteResourceByField(ctx context.Context, model M, field string, value any) error
}

type Repository interface {
	SpecRepository
	Zones() GenericRepository[*metadata.Zone]
	DomainEndpoints() GenericRepository[*metadata.DomainEndpoint]
	DomainEndpointStatuses() GenericRepository[*metadata.DomainEndpointStatus]
	ServiceBindings() GenericRepository[*metadata.ServiceBinding]
	DNSProjections() GenericRepository[*metadata.DNSProjection]
	DNSRecords() GenericRepository[*metadata.DNSRecord]
	RouteProjections() GenericRepository[*metadata.RouteProjection]
	CertificateRevisions() GenericRepository[*metadata.CertificateRevision]
	ACMEOrders() GenericRepository[*metadata.ACMEOrder]
	ACMEChallenges() GenericRepository[*metadata.ACMEChallenge]
	Nodes() GenericRepository[*metadata.Node]
	Attachments() GenericRepository[*metadata.Attachment]
}
