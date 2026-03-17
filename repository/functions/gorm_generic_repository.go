package functions

import (
	"context"

	"gorm.io/gorm"
	"gorm.io/gorm/clause"
)

type GormGenericRepository[M any] struct {
	db *gorm.DB
}

func (r *GormGenericRepository[M]) ListResource(ctx context.Context, out any, orderBy string) error {
	return r.db.WithContext(ctx).Where("deleted = ?", false).Order(orderBy).Find(out).Error
}

func (r *GormGenericRepository[M]) GetResourceByField(ctx context.Context, out M, field string, value any) error {
	return r.db.WithContext(ctx).Where("deleted = ?", false).First(out, field+" = ?", value).Error
}

func (r *GormGenericRepository[M]) UpsertResource(ctx context.Context, model M) error {
	return r.db.WithContext(ctx).Clauses(clause.OnConflict{UpdateAll: true}).Create(model).Error
}

func (r *GormGenericRepository[M]) DeleteResourceByField(ctx context.Context, model M, field string, value any) error {
	return r.db.WithContext(ctx).Model(model).Where("deleted = ?", false).Where(field+" = ?", value).Update("deleted", true).Error
}
