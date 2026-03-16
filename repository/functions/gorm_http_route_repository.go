package functions

import (
	"context"

	"github.com/jabberwocky238/luna-edge/repository/metadata"
)

func (r *GormRepository) HTTPRoutes() GenericRepository[*metadata.HTTPRoute] {
	return &gormGenericRepository[*metadata.HTTPRoute]{db: r.db}
}

func (r *GormRepository) ListHTTPRoutesByDomainID(ctx context.Context, domainID string) ([]metadata.HTTPRoute, error) {
	var routes []metadata.HTTPRoute
	err := r.db.WithContext(ctx).
		Order("priority desc, length(path) desc, id asc").
		Find(&routes, "domain_id = ?", domainID).Error
	return routes, err
}

func (r *GormRepository) GetHTTPRouteByHostname(ctx context.Context, hostname, requestPath string) (*metadata.HTTPRoute, error) {
	var routes []metadata.HTTPRoute
	if err := r.db.WithContext(ctx).
		Order("priority desc, length(path) desc, id asc").
		Find(&routes, "hostname = ?", hostname).Error; err != nil {
		return nil, err
	}
	for i := range routes {
		path := routes[i].Path
		if path == "" || path == "/" {
			return &routes[i], nil
		}
		if len(requestPath) >= len(path) && requestPath[:len(path)] == path {
			return &routes[i], nil
		}
	}
	return nil, nil
}
