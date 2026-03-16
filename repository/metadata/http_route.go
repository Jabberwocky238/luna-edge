package metadata

import "time"

// HTTPRoute 表示一个 L7 HTTP 路由规则。
type HTTPRoute struct {
	ID string `json:"id" gorm:"column:id;primaryKey;type:varchar(64)"`
	// DomainID 是所属域名入口对象 ID。
	DomainID string `json:"domain_id" gorm:"column:domain_id;not null;index;type:varchar(64)"`
	// Hostname 是该路由命中的完整域名。
	Hostname string `json:"hostname" gorm:"column:hostname;not null;index;type:varchar(255)"`
	// Path 是该路由的请求路径前缀。
	Path string `json:"path" gorm:"column:path;not null;default:'/';type:varchar(1024)"`
	// Priority 越大优先级越高。
	Priority int32 `json:"priority" gorm:"column:priority;not null;default:0"`
	// BindingID 指向一个 ServiceBinding。
	BindingID string `json:"binding_id" gorm:"column:binding_id;not null;index;type:varchar(64)"`
	// RouteVersion 是该路由规则的版本号。
	RouteVersion uint64 `json:"route_version" gorm:"column:route_version;not null;default:1"`
	// Listener 是路由挂载到的入口监听器名称。
	Listener string `json:"listener" gorm:"column:listener;not null;default:'';type:varchar(128)"`
	// RouteJSON 保存来源路由规则的附加 JSON。
	RouteJSON string `json:"route_json" gorm:"column:route_json;not null;default:'';type:text"`
	UpdatedAt time.Time `json:"updated_at" gorm:"column:updated_at;not null;autoUpdateTime"`
}

func (HTTPRoute) TableName() string {
	return "http_routes"
}
