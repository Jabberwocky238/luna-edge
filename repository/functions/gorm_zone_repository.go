package functions

import (
	"context"

	"github.com/jabberwocky238/luna-edge/repository/metadata"
)

func (r *GormRepository) Zones() GenericRepository[*metadata.Zone] {
	return &gormGenericRepository[*metadata.Zone]{db: r.db}
}

func (r *GormRepository) UpsertZone(ctx context.Context, zone *metadata.Zone) error {
	return r.Zones().UpsertResource(ctx, zone)
}

func (r *GormRepository) GetZoneByID(ctx context.Context, id string) (*metadata.Zone, error) {
	zone := &metadata.Zone{}
	if err := r.Zones().GetResourceByField(ctx, zone, "id", id); err != nil {
		return nil, err
	}
	return zone, nil
}

func (r *GormRepository) GetZoneByName(ctx context.Context, name string) (*metadata.Zone, error) {
	zone := &metadata.Zone{}
	if err := r.db.WithContext(ctx).First(zone, "name = ?", name).Error; err != nil {
		return nil, err
	}
	return zone, nil
}

func (r *GormRepository) ListZones(ctx context.Context) ([]metadata.Zone, error) {
	var zones []metadata.Zone
	err := r.Zones().ListResource(ctx, &zones, "name asc")
	return zones, err
}
