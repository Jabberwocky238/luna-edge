package metadata

import "time"

// ServiceBinding 表示域名入口最终绑定到的服务目标。
type ServiceBinding struct {
	// ID 是服务绑定对象的唯一标识。
	ID            string    `json:"id" gorm:"column:id;primaryKey;type:varchar(64)"`
	// DomainID 是该绑定所属域名入口对象的 ID。
	DomainID      string    `json:"domain_id" gorm:"column:domain_id;not null;index;type:varchar(64)"`
	// Hostname 是该绑定对应的域名。
	Hostname      string    `json:"hostname" gorm:"column:hostname;not null;index;type:varchar(255)"`
	// ServiceID 是逻辑服务的唯一标识。
	ServiceID     string    `json:"service_id" gorm:"column:service_id;not null;index;type:varchar(128)"`
	// Namespace 是服务所属命名空间，兼容 k8s 风格来源。
	Namespace     string    `json:"namespace" gorm:"column:namespace;not null;default:'';type:varchar(128)"`
	// Name 是服务名称。
	Name          string    `json:"name" gorm:"column:name;not null;default:'';type:varchar(128)"`
	// Address 是服务的上游地址。
	Address       string    `json:"address" gorm:"column:address;not null;default:'';type:varchar(255)"`
	// Port 是服务监听端口。
	Port          uint32    `json:"port" gorm:"column:port;not null;default:0"`
	// Protocol 是服务协议，例如 http 或 tcp。
	Protocol      string    `json:"protocol" gorm:"column:protocol;not null;default:http;type:varchar(32)"`
	// RouteVersion 是该绑定对应的路由版本号。
	RouteVersion  uint64    `json:"route_version" gorm:"column:route_version;not null;default:1"`
	// BackendJSON 是后端详细配置的 JSON 文本。
	BackendJSON   string    `json:"backend_json" gorm:"column:backend_json;not null;default:'';type:text"`
	// CreatedAt 是绑定记录创建时间。
	CreatedAt     time.Time `json:"created_at" gorm:"column:created_at;not null;autoCreateTime"`
	// UpdatedAt 是绑定记录最后一次更新时间。
	UpdatedAt     time.Time `json:"updated_at" gorm:"column:updated_at;not null;autoUpdateTime"`
}

func (ServiceBinding) TableName() string {
	return "service_bindings"
}
