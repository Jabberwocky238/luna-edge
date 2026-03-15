package functions

import (
	"context"

	"github.com/jabberwocky238/luna-edge/repository/metadata"
)

func (r *GormRepository) RouteProjections() GenericRepository[*metadata.RouteProjection] {
	return &gormGenericRepository[*metadata.RouteProjection]{db: r.db}
}

func (r *GormRepository) UpsertRouteProjection(ctx context.Context, route *metadata.RouteProjection) error {
	return r.RouteProjections().UpsertResource(ctx, route)
}

func (r *GormRepository) GetRouteProjection(ctx context.Context, domainID string) (*metadata.RouteProjection, error) {
	route := &metadata.RouteProjection{}
	if err := r.RouteProjections().GetResourceByField(ctx, route, "domain_id", domainID); err != nil {
		return nil, err
	}
	return route, nil
}
