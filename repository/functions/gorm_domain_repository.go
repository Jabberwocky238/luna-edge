package functions

import (
	"context"

	"github.com/jabberwocky238/luna-edge/repository/metadata"
)

func (r *GormRepository) DomainEndpoints() GenericRepository[*metadata.DomainEndpoint] {
	return &GormGenericRepository[*metadata.DomainEndpoint]{db: r.db}
}

func (r *GormRepository) GetDomainEndpointByID(ctx context.Context, id string) (*metadata.DomainEndpoint, error) {
	domain := &metadata.DomainEndpoint{}
	if err := r.db.WithContext(ctx).
		Where("deleted = ?", false).
		First(domain, "id = ?", id).Error; err != nil {
		return nil, err
	}
	return domain, nil
}

func (r *GormRepository) GetDomainEndpointByHostname(ctx context.Context, hostname string) (*metadata.DomainEndpoint, error) {
	domain := &metadata.DomainEndpoint{}
	if err := r.db.WithContext(ctx).
		Where("deleted = ?", false).
		First(domain, "hostname = ?", hostname).Error; err != nil {
		return nil, err
	}
	return domain, nil
}
