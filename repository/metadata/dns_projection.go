package metadata

import "time"

// DNSProjection 表示某个域名入口对象整体的 DNS 物化结果摘要。
type DNSProjection struct {
	// DomainID 是对应的域名入口对象 ID。
	DomainID    string    `json:"domain_id" gorm:"column:domain_id;primaryKey;type:varchar(64)"`
	// ZoneID 是所属 Zone 的 ID。
	ZoneID      string    `json:"zone_id" gorm:"column:zone_id;not null;index;type:varchar(64)"`
	// Version 是当前 DNS 物化结果的版本号。
	Version     uint64    `json:"version" gorm:"column:version;not null;default:1"`
	// Projection 是完整 DNS 物化内容的 JSON 文本。
	Projection  string    `json:"projection_json" gorm:"column:projection_json;not null;default:'';type:text"`
	// Checksum 是物化内容的校验摘要。
	Checksum    string    `json:"checksum" gorm:"column:checksum;not null;default:'';type:varchar(128)"`
	// UpdatedAt 是该 DNS 物化摘要最后一次更新时间。
	UpdatedAt   time.Time `json:"updated_at" gorm:"column:updated_at;not null;autoUpdateTime"`
}

func (DNSProjection) TableName() string {
	return "dns_projections"
}
