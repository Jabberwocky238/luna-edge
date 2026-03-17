package metadata

// 三种情况，L4 TLS passthrough，L4 TLS termination，L7 HTTPS，L7 HTTP。

type BackendType string

const (
	// BackendTypeL4TLSPassthrough 表示该域名入口走 L4 TLS passthrough 路由模型。
	BackendTypeL4TLSPassthrough BackendType = "l4-tls-passthrough"
	// BackendTypeL4TLSTermination 表示该域名入口走 L4 TLS termination 路由模型。
	BackendTypeL4TLSTermination BackendType = "l4-tls-termination"
	// BackendTypeL7HTTPS 表示该域名入口走 L7 HTTPS 路由模型。
	BackendTypeL7HTTPS BackendType = "l7-https"
	// BackendTypeL7HTTP 表示该域名入口走 L7 HTTP 路由模型。
	BackendTypeL7HTTP BackendType = "l7-http"
	// BackendTypeL7HTTPBoth 表示该域名入口走 L7 HTTP + HTTPS 路由模型。
	BackendTypeL7HTTPBoth BackendType = "l7-http-both"
)

// DomainEndpoint 表示系统的一等资源，即一个域名入口对象。
type DomainEndpoint struct {
	Shared
	// ID 是域名入口对象的唯一标识。
	ID string `json:"id" gorm:"column:id;primaryKey;type:text"`
	// Hostname 是该入口对象管理的完整域名。
	Hostname string `json:"hostname" gorm:"column:hostname;not null;uniqueIndex;type:text"`
	// 是否需要证书
	NeedCert bool `json:"need_cert" gorm:"column:need_cert;not null;default:false"`
	// 最新证书
	BackendType     BackendType `json:"backend_type" gorm:"column:backend_type;not null;type:text"`
	BindedServiceID string      `json:"binded_service_ref" gorm:"column:binded_service_ref;type:text"`
}

func (DomainEndpoint) TableName() string {
	return "domain_endpoints"
}
