package metadata

// HTTPRoute 表示一个 L7 HTTP 路由规则。
type HTTPRoute struct {
	Shared
	ID string `json:"id" gorm:"column:id;primaryKey;type:text"`
	// DomainEndpointID 是所属域名入口对象 ID。
	DomainEndpointID string `json:"domain_endpoint_id" gorm:"column:domain_endpoint_id;not null;index;type:text"`
	// Hostname 是该路由命中的完整域名。
	Hostname string `json:"hostname" gorm:"column:hostname;not null;index;type:text"`
	// Path 是该路由的请求路径前缀。
	Path string `json:"path" gorm:"column:path;not null;default:'/';type:text"`
	// Priority 越大优先级越高。
	Priority int32 `json:"priority" gorm:"column:priority;not null;default:0"`
	// BackendRefID 指向一个 ServiceBackendRef。
	BackendRefID string `json:"backend_ref_id" gorm:"column:backend_ref_id;not null;index;type:text"`
}

func (HTTPRoute) TableName() string {
	return "http_routes"
}

// HTTPRouteProjection 是 HTTPRoute 的查询投影类型，包含了关联的 ServiceBackendRef 信息。
type HTTPRouteProjection struct {
	Shared
	ID string `json:"id" gorm:"column:id;primaryKey;type:text"`
	// DomainEndpointID 是所属域名入口对象 ID。
	DomainEndpointID string `json:"domain_endpoint_id" gorm:"column:domain_endpoint_id;not null;index;type:text"`
	// Hostname 是该路由命中的完整域名。
	Hostname string `json:"hostname" gorm:"column:hostname;not null;index;type:text"`
	// Path 是该路由的请求路径前缀。
	Path string `json:"path" gorm:"column:path;not null;default:'/';type:text"`
	// Priority 越大优先级越高。
	Priority int32 `json:"priority" gorm:"column:priority;not null;default:0"`
	// BackendRefID 指向一个 ServiceBackendRef。
	BackendRef *ServiceBackendRef `json:"backend_ref" gorm:"foreignKey:BackendRefID"`
}
