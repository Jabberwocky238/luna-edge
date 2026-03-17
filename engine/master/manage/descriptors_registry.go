package manage

import (
	"context"

	"github.com/jabberwocky238/luna-edge/repository/metadata"
)

func init() {
	descriptors["certificate_revisions"] = descriptor{
		newModel: func() any { return &metadata.CertificateRevision{} },
		idField:  "id",
		afterUpsert: func(ctx context.Context, w *Wrapper, model any) error {
			cert := model.(*metadata.CertificateRevision)
			return publishCertificate(ctx, w, cert.DomainEndpointID, cert.Revision)
		},
		afterDelete: func(ctx context.Context, w *Wrapper, model any) error {
			cert := model.(*metadata.CertificateRevision)
			return publishDeleteForDomain(ctx, w, cert.DomainEndpointID, nil, cert.ID)
		},
	}

	descriptors["dns_records"] = descriptor{
		newModel:    func() any { return &metadata.DNSRecord{} },
		idField:     "id",
		afterUpsert: publishAllModel,
		afterDelete: publishAllModel,
	}

	descriptors["domain_endpoints"] = descriptor{
		newModel: func() any { return &metadata.DomainEndpoint{} },
		idField:  "id",
		afterUpsert: func(ctx context.Context, w *Wrapper, model any) error {
			return publishDomain(ctx, w, model.(*metadata.DomainEndpoint).ID)
		},
		afterDelete: func(ctx context.Context, w *Wrapper, model any) error {
			domain := model.(*metadata.DomainEndpoint)
			return publishDeleteForDomain(ctx, w, domain.ID, nil, domain.Hostname)
		},
	}

	descriptors["http_routes"] = descriptor{
		newModel: func() any { return &metadata.HTTPRoute{} },
		idField:  "id",
		afterUpsert: func(ctx context.Context, w *Wrapper, model any) error {
			return publishRoute(ctx, w, model.(*metadata.HTTPRoute).DomainEndpointID, "")
		},
		afterDelete: func(ctx context.Context, w *Wrapper, model any) error {
			route := model.(*metadata.HTTPRoute)
			return publishDomain(ctx, w, route.DomainEndpointID)
		},
	}

	descriptors["service_backend_refs"] = descriptor{
		newModel:    func() any { return &metadata.ServiceBackendRef{} },
		idField:     "id",
		afterUpsert: publishAllModel,
		afterDelete: publishAllModel,
	}

	descriptors["snapshot_records"] = descriptor{
		newModel:    func() any { return &metadata.SnapshotRecord{} },
		idField:     "id",
		afterUpsert: noopBroadcast,
		afterDelete: noopBroadcast,
	}
}
