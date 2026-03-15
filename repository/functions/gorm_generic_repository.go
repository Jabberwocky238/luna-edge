package functions

import (
	"context"

	"gorm.io/gorm"
	"gorm.io/gorm/clause"
)

type gormGenericRepository[M any] struct {
	db *gorm.DB
}

func (r *gormGenericRepository[M]) ListResource(ctx context.Context, out any, orderBy string) error {
	return r.db.WithContext(ctx).Order(orderBy).Find(out).Error
}

func (r *gormGenericRepository[M]) GetResourceByField(ctx context.Context, out M, field string, value any) error {
	return r.db.WithContext(ctx).First(out, field+" = ?", value).Error
}

func (r *gormGenericRepository[M]) UpsertResource(ctx context.Context, model M) error {
	return r.db.WithContext(ctx).Clauses(clause.OnConflict{UpdateAll: true}).Create(model).Error
}

func (r *gormGenericRepository[M]) DeleteResourceByField(ctx context.Context, model M, field string, value any) error {
	return r.db.WithContext(ctx).Delete(model, field+" = ?", value).Error
}
