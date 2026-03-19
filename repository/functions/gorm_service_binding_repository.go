package functions

import (
	"context"

	"github.com/jabberwocky238/luna-edge/repository/metadata"
)

func (r *GormRepository) ServiceBindingRefs() GenericRepository[*metadata.ServiceBackendRef] {
	return &GormGenericRepository[*metadata.ServiceBackendRef]{db: r.db}
}

func (r *GormRepository) ListServiceBindingsByHostname(ctx context.Context, hostname string) ([]metadata.ServiceBackendRef, error) {
	var backends []metadata.ServiceBackendRef
	err := r.db.WithContext(ctx).
		Table((&metadata.ServiceBackendRef{}).TableName()+" AS backend_refs").
		Joins("JOIN "+(&metadata.HTTPRoute{}).TableName()+" AS routes ON routes.backend_ref_id = backend_refs.id").
		Where("backend_refs.deleted = ?", false).
		Where("routes.deleted = ?", false).
		Where("routes.hostname = ?", hostname).
		Order("backend_refs.id asc").
		Find(&backends).Error
	return backends, err
}
