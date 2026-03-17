package metadata

// 这个类型不应该通过数据库存储，而是查询中间态，从而避免不必要的数据库表和字段
type DomainEntryProjection struct {
	ID               string                `json:"id"`
	Hostname         string                `json:"hostname"`
	Cert             *CertificateRevision  `json:"cert"`
	BackendType      BackendType           `json:"backend_type"`
	HTTPRoutes       []HTTPRouteProjection `json:"http_routes"`
	BindedBackendRef *ServiceBackendRef    `json:"binded_backend_ref"`
}
