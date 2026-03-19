package functions

import (
	"context"

	"github.com/jabberwocky238/luna-edge/repository/metadata"
)

func (r *GormRepository) ServiceBindingRefs() GenericRepository[*metadata.ServiceBackendRef] {
	return &GormGenericRepository[*metadata.ServiceBackendRef]{db: r.db}
}

func (r *GormRepository) GetServiceBindingByDomainID(ctx context.Context, domainID string) (*metadata.ServiceBackendRef, error) {
	route := &metadata.HTTPRoute{}
	if err := r.db.WithContext(ctx).
		Where("deleted = ?", false).
		Order("priority desc, length(path) desc, id asc").
		First(route, "domain_endpoint_id = ?", domainID).Error; err != nil {
		return nil, err
	}

	backend := &metadata.ServiceBackendRef{}
	if err := r.db.WithContext(ctx).
		Where("deleted = ?", false).
		First(backend, "id = ?", route.BackendRefID).Error; err != nil {
		return nil, err
	}
	return backend, nil
}

func (r *GormRepository) ListServiceBindingsByHostname(ctx context.Context, domainID string) ([]metadata.ServiceBackendRef, error) {
	var backends []metadata.ServiceBackendRef
	err := r.db.WithContext(ctx).
		Table((&metadata.ServiceBackendRef{}).TableName()+" AS backend_refs").
		Joins("JOIN "+(&metadata.HTTPRoute{}).TableName()+" AS routes ON routes.backend_ref_id = backend_refs.id").
		Where("backend_refs.deleted = ?", false).
		Where("routes.deleted = ?", false).
		Where("routes.domain_endpoint_id = ?", domainID).
		Order("backend_refs.id asc").
		Find(&backends).Error
	return backends, err
}
