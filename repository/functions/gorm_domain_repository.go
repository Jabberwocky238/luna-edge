package functions

import (
	"context"

	"github.com/jabberwocky238/luna-edge/repository/metadata"
)

func (r *GormRepository) DomainEndpoints() GenericRepository[*metadata.DomainEndpoint] {
	return &gormGenericRepository[*metadata.DomainEndpoint]{db: r.db}
}

func (r *GormRepository) DomainEndpointStatuses() GenericRepository[*metadata.DomainEndpointStatus] {
	return &gormGenericRepository[*metadata.DomainEndpointStatus]{db: r.db}
}

func (r *GormRepository) UpsertDomainEndpoint(ctx context.Context, domain *metadata.DomainEndpoint) error {
	return r.DomainEndpoints().UpsertResource(ctx, domain)
}

func (r *GormRepository) GetDomainEndpointByID(ctx context.Context, id string) (*metadata.DomainEndpoint, error) {
	domain := &metadata.DomainEndpoint{}
	if err := r.DomainEndpoints().GetResourceByField(ctx, domain, "id", id); err != nil {
		return nil, err
	}
	return domain, nil
}

func (r *GormRepository) GetDomainEndpointByHostname(ctx context.Context, hostname string) (*metadata.DomainEndpoint, error) {
	domain := &metadata.DomainEndpoint{}
	if err := r.db.WithContext(ctx).First(domain, "hostname = ?", hostname).Error; err != nil {
		return nil, err
	}
	return domain, nil
}

func (r *GormRepository) ListDomainEndpoints(ctx context.Context) ([]metadata.DomainEndpoint, error) {
	var domains []metadata.DomainEndpoint
	err := r.DomainEndpoints().ListResource(ctx, &domains, "hostname asc")
	return domains, err
}

func (r *GormRepository) UpsertDomainEndpointStatus(ctx context.Context, status *metadata.DomainEndpointStatus) error {
	return r.DomainEndpointStatuses().UpsertResource(ctx, status)
}

func (r *GormRepository) GetDomainEndpointStatus(ctx context.Context, domainID string) (*metadata.DomainEndpointStatus, error) {
	status := &metadata.DomainEndpointStatus{}
	if err := r.DomainEndpointStatuses().GetResourceByField(ctx, status, "domain_endpoint_id", domainID); err != nil {
		return nil, err
	}
	return status, nil
}
