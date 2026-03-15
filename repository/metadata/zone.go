package metadata

import "time"

// Zone 表示一个可管理的 DNS 区域。
type Zone struct {
	// ID 是 Zone 对象的唯一标识。
	ID                  string    `json:"id" gorm:"column:id;primaryKey;type:varchar(64)"`
	// Name 是区域名称，通常为 example.com 这种 zone 名。
	Name                string    `json:"name" gorm:"column:name;not null;uniqueIndex;type:varchar(255)"`
	// Kind 表示区域类型，例如 public 或 private。
	Kind                string    `json:"kind" gorm:"column:kind;not null;default:public;type:varchar(32)"`
	// DNSMode 表示该区域的 DNS 工作模式，例如 authoritative 或 delegated。
	DNSMode             string    `json:"dns_mode" gorm:"column:dns_mode;not null;default:authoritative;type:varchar(32)"`
	// DefaultACMEProvider 是该区域默认使用的 ACME 提供方。
	DefaultACMEProvider string    `json:"default_acme_provider" gorm:"column:default_acme_provider;not null;default:'';type:varchar(128)"`
	// LabelsJSON 是区域附加标签的 JSON 文本。
	LabelsJSON          string    `json:"labels_json" gorm:"column:labels_json;not null;default:'';type:text"`
	// CreatedAt 是区域记录创建时间。
	CreatedAt           time.Time `json:"created_at" gorm:"column:created_at;not null;autoCreateTime"`
	// UpdatedAt 是区域记录最后一次更新时间。
	UpdatedAt           time.Time `json:"updated_at" gorm:"column:updated_at;not null;autoUpdateTime"`
}

func (Zone) TableName() string {
	return "zones"
}
