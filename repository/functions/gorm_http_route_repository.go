package functions

import (
	"context"

	"github.com/jabberwocky238/luna-edge/repository/metadata"
)

func (r *GormRepository) HTTPRoutes() GenericRepository[*metadata.HTTPRoute] {
	return &GormGenericRepository[*metadata.HTTPRoute]{db: r.db}
}

func (r *GormRepository) ListHTTPRoutesByHostname(ctx context.Context, hostname string) ([]metadata.HTTPRoute, error) {
	var routes []metadata.HTTPRoute
	err := r.db.WithContext(ctx).
		Where("deleted = ?", false).
		Order("priority desc, length(path) desc, id asc").
		Find(&routes, "hostname = ?", hostname).Error
	return routes, err
}
