package metadata

import "time"

// DomainEndpoint 表示系统的一等资源，即一个域名入口对象。
type DomainEndpoint struct {
	// ID 是域名入口对象的唯一标识。
	ID           string    `json:"id" gorm:"column:id;primaryKey;type:varchar(64)"`
	// ZoneID 是该域名所属 Zone 的 ID。
	ZoneID       string    `json:"zone_id" gorm:"column:zone_id;not null;index;type:varchar(64)"`
	// Hostname 是该入口对象管理的完整域名。
	Hostname     string    `json:"hostname" gorm:"column:hostname;not null;uniqueIndex;type:varchar(255)"`
	// BackendType 表示该域名入口走 L4 还是 L7 路由模型。
	BackendType  string    `json:"backend_type" gorm:"column:backend_type;not null;default:l4;type:varchar(16)"`
	// SpecJSON 是该域名入口声明态规格的 JSON 文本。
	SpecJSON     string    `json:"spec_json" gorm:"column:spec_json;not null;default:'';type:text"`
	// SpecHash 是声明态规格内容的摘要，用于快速判断是否变更。
	SpecHash     string    `json:"spec_hash" gorm:"column:spec_hash;not null;default:'';type:varchar(128)"`
	// Generation 是用户期望态版本号，每次规格变更时递增。
	Generation   uint64    `json:"generation" gorm:"column:generation;not null;default:1"`
	// StateVersion 是内部状态版本号，用于订阅和物化更新。
	StateVersion uint64    `json:"state_version" gorm:"column:state_version;not null;default:1"`
	// Deleted 表示该对象是否已进入逻辑删除状态。
	Deleted      bool      `json:"deleted" gorm:"column:deleted;not null;default:false"`
	// CreatedAt 是该对象的创建时间。
	CreatedAt    time.Time `json:"created_at" gorm:"column:created_at;not null;autoCreateTime"`
	// UpdatedAt 是该对象最后一次更新时间。
	UpdatedAt    time.Time `json:"updated_at" gorm:"column:updated_at;not null;autoUpdateTime"`
}

func (DomainEndpoint) TableName() string {
	return "domain_endpoints"
}
