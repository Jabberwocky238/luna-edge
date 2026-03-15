package functions

import (
	"context"

	"github.com/jabberwocky238/luna-edge/repository/metadata"
)

func (r *GormRepository) ServiceBindings() GenericRepository[*metadata.ServiceBinding] {
	return &gormGenericRepository[*metadata.ServiceBinding]{db: r.db}
}

func (r *GormRepository) UpsertServiceBinding(ctx context.Context, binding *metadata.ServiceBinding) error {
	return r.ServiceBindings().UpsertResource(ctx, binding)
}

func (r *GormRepository) GetServiceBindingByDomainID(ctx context.Context, domainID string) (*metadata.ServiceBinding, error) {
	binding := &metadata.ServiceBinding{}
	if err := r.db.WithContext(ctx).First(binding, "domain_id = ?", domainID).Error; err != nil {
		return nil, err
	}
	return binding, nil
}

func (r *GormRepository) GetServiceBindingByHostname(ctx context.Context, hostname string) (*metadata.ServiceBinding, error) {
	binding := &metadata.ServiceBinding{}
	if err := r.db.WithContext(ctx).First(binding, "hostname = ?", hostname).Error; err != nil {
		return nil, err
	}
	return binding, nil
}
