package metadata

import (
	"time"

	"gorm.io/gorm"
)

// AllModels 返回所有需要参与迁移的元数据模型。
func AllModels() []interface{} {
	return []interface{}{
		&Zone{},
		&DomainEndpoint{},
		&DomainEndpointStatus{},
		&ServiceBinding{},
		&DNSRecord{},
		&CertificateRevision{},
		&ACMEOrder{},
		&ACMEChallenge{},
		&Node{},
		&Attachment{},
	}
}

// AutoMigrate 对所有元数据模型执行自动迁移。
func AutoMigrate(db *gorm.DB) error {
	return db.AutoMigrate(AllModels()...)
}

type Shared struct {
	// 根据engine替我增长的epoch标志着snapshot大版本
	SyncEpoch int64 `json:"sync_epoch" gorm:"column:sync_epoch;not null"`
	// 根据engine替我增长的index标志着snapshot小版本，1000为一个周期
	SyncIndex int64 `json:"sync_index" gorm:"column:sync_index;not null"`
	// CreatedAt 是节点记录创建时间。
	CreatedAt time.Time `json:"created_at" gorm:"column:created_at;not null;autoCreateTime"`
	// UpdatedAt 是节点记录最后一次更新时间。
	UpdatedAt time.Time `json:"updated_at" gorm:"column:updated_at;not null;autoUpdateTime"`
}
