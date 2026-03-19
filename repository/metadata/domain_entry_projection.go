package metadata

// 这个类型不应该通过数据库存储，而是查询中间态，从而避免不必要的数据库表和字段
type DomainEntryProjection struct {
	Hostname         string                `json:"hostname"`
	Deleted          bool                  `json:"deleted"`
	NeedCert         bool                  `json:"need_cert"`
	Cert             *CertificateRevision  `json:"cert"`
	BackendType      BackendType           `json:"backend_type"`
	HTTPRoutes       []HTTPRouteProjection `json:"http_routes"`
	BindedBackendRef *ServiceBackendRef    `json:"binded_backend_ref"`
}

// HTTPRouteProjection 是 HTTPRoute 的查询投影类型，包含了关联的 ServiceBackendRef 信息。
type HTTPRouteProjection struct {
	ID         string             `json:"id" gorm:"column:id;primaryKey;type:text"`
	Path       string             `json:"path" gorm:"column:path;not null;default:'/';type:text"`
	Priority   int32              `json:"priority" gorm:"column:priority;not null;default:0"`
	BackendRef *ServiceBackendRef `json:"backend_ref" gorm:"foreignKey:BackendRefID"`
}
