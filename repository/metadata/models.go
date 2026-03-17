package metadata

import (
	"time"

	"gorm.io/gorm"
)

// AllModels 返回所有需要参与迁移的元数据模型。
func AllModels() []interface{} {
	return []interface{}{
		&DomainEndpoint{},
		&ServiceBackendRef{},
		&HTTPRoute{},
		&DNSRecord{},
		&CertificateRevision{},
	}
}

// AutoMigrate 对所有元数据模型执行自动迁移。
func AutoMigrate(db *gorm.DB) error {
	return db.AutoMigrate(AllModels()...)
}

type Shared struct {
	// 逻辑删除位
	Deleted bool `json:"deleted" gorm:"column:deleted;not null;default:false"`
	// CreatedAt 是节点记录创建时间。
	CreatedAt time.Time `json:"created_at" gorm:"column:created_at;not null;autoCreateTime"`
	// UpdatedAt 是节点记录最后一次更新时间。
	UpdatedAt time.Time `json:"updated_at" gorm:"column:updated_at;not null;autoUpdateTime"`
}
