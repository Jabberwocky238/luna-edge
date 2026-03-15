package metadata

import "time"

// RouteProjection 表示某个域名入口对象的入口路由物化结果。
type RouteProjection struct {
	// DomainID 是对应域名入口对象的 ID。
	DomainID string `json:"domain_id" gorm:"column:domain_id;primaryKey;type:varchar(64)"`
	// Hostname 是该路由投影对应的完整域名。
	Hostname string `json:"hostname" gorm:"column:hostname;not null;index;type:varchar(255)"`
	// RouteVersion 是当前路由投影的版本号。
	RouteVersion uint64 `json:"route_version" gorm:"column:route_version;not null;default:1"`
	// Protocol 是入口协议，例如 http、https、tcp。
	Protocol ServiceBindingRouteKind `json:"protocol" gorm:"column:protocol;not null;default:http;type:varchar(32)"`
	// RouteJSON 是完整路由内容的 JSON 文本。
	RouteJSON string `json:"route_json" gorm:"column:route_json;not null;default:'';type:text"`
	// BindingID 是该路由关联的服务绑定 ID。
	BindingID string `json:"binding_id" gorm:"column:binding_id;not null;default:'';index;type:varchar(64)"`
	// UpdatedAt 是该路由投影最后一次更新时间。
	UpdatedAt time.Time `json:"updated_at" gorm:"column:updated_at;not null;autoUpdateTime"`
}

func (RouteProjection) TableName() string {
	return "route_projections"
}
