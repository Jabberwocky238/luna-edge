package manage

import (
	"context"

	"github.com/jabberwocky238/luna-edge/repository/metadata"
)

func init() {
	descriptors["acme_challenges"] = descriptor{
		newModel:    func() any { return &metadata.ACMEChallenge{} },
		idField:     "id",
		afterUpsert: noopBroadcast,
		afterDelete: noopBroadcast,
	}

	descriptors["acme_orders"] = descriptor{
		newModel:    func() any { return &metadata.ACMEOrder{} },
		idField:     "id",
		afterUpsert: noopBroadcast,
		afterDelete: noopBroadcast,
	}

	descriptors["attachments"] = descriptor{
		newModel: func() any { return &metadata.Attachment{} },
		idField:  "id",
		afterUpsert: func(ctx context.Context, w *Wrapper, model any) error {
			return publishAssignment(ctx, w, model.(*metadata.Attachment))
		},
		afterDelete: func(ctx context.Context, w *Wrapper, model any) error {
			attachment := model.(*metadata.Attachment)
			return publishDelete(ctx, w, attachment.NodeID, nil, attachment.ID)
		},
	}

	descriptors["certificate_revisions"] = descriptor{
		newModel: func() any { return &metadata.CertificateRevision{} },
		idField:  "id",
		afterUpsert: func(ctx context.Context, w *Wrapper, model any) error {
			cert := model.(*metadata.CertificateRevision)
			return publishCertificate(ctx, w, cert.DomainID, cert.Revision)
		},
		afterDelete: func(ctx context.Context, w *Wrapper, model any) error {
			cert := model.(*metadata.CertificateRevision)
			return publishDeleteForDomain(ctx, w, cert.DomainID, nil, cert.ID)
		},
	}

	descriptors["dns_records"] = descriptor{
		newModel: func() any { return &metadata.DNSRecord{} },
		idField:  "id",
		afterUpsert: func(ctx context.Context, w *Wrapper, model any) error {
			return publishAssignmentsForDomain(ctx, w, model.(*metadata.DNSRecord).DomainID)
		},
		afterDelete: func(ctx context.Context, w *Wrapper, model any) error {
			return publishAssignmentsForDomain(ctx, w, model.(*metadata.DNSRecord).DomainID)
		},
	}

	descriptors["domain_endpoint_status"] = descriptor{
		newModel: func() any { return &metadata.DomainEndpointStatus{} },
		idField:  "domain_endpoint_id",
		afterUpsert: func(ctx context.Context, w *Wrapper, model any) error {
			return publishDomain(ctx, w, model.(*metadata.DomainEndpointStatus).DomainEndpointID)
		},
		afterDelete: noopBroadcast,
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
			return publishRoute(ctx, w, model.(*metadata.HTTPRoute).DomainID, "")
		},
		afterDelete: func(ctx context.Context, w *Wrapper, model any) error {
			route := model.(*metadata.HTTPRoute)
			return publishDeleteForDomain(ctx, w, route.DomainID, nil, route.Hostname)
		},
	}

	descriptors["nodes"] = descriptor{
		newModel: func() any { return &metadata.Node{} },
		idField:  "id",
		afterUpsert: func(ctx context.Context, w *Wrapper, model any) error {
			return publishAssignmentsForNode(ctx, w, model.(*metadata.Node).ID)
		},
		afterDelete: noopBroadcast,
	}

	descriptors["service_bindings"] = descriptor{
		newModel: func() any { return &metadata.ServiceBinding{} },
		idField:  "id",
		afterUpsert: func(ctx context.Context, w *Wrapper, model any) error {
			return publishBinding(ctx, w, model.(*metadata.ServiceBinding).DomainID)
		},
		afterDelete: func(ctx context.Context, w *Wrapper, model any) error {
			binding := model.(*metadata.ServiceBinding)
			return publishDeleteForDomain(ctx, w, binding.DomainID, nil, binding.Hostname)
		},
	}

	descriptors["zones"] = descriptor{
		newModel:    func() any { return &metadata.Zone{} },
		idField:     "id",
		afterUpsert: noopBroadcast,
		afterDelete: noopBroadcast,
	}
}
