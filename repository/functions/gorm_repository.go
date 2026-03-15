package functions

import "gorm.io/gorm"

// GormRepository 是基于 GORM 的统一仓储实现。
type GormRepository struct {
	db *gorm.DB
}

// NewGormRepository 创建一个基于 GORM 的仓储实现。
func NewGormRepository(db *gorm.DB) Repository {
	return &GormRepository{db: db}
}
