package metadata

type ServiceBackendType string

const (
	ServiceBackendTypeSVC      ServiceBackendType = "SVC"
	ServiceBackendTypeExternal ServiceBackendType = "EXTERNAL"
)

// ServiceBackendRef 表示一个声明式的 Kubernetes Service 后端引用。
//
// 它只保存 Gateway API backendRef 的事实字段，不再混入域名、地址解析、
// 协议、版本或快照等物化语义。
type ServiceBackendRef struct {
	Shared
	// ID 是后端引用对象的唯一标识。
	ID string `json:"id" gorm:"column:id;primaryKey;type:text"`
	// Type 是后端引用类型，支持 SVC 和 EXTERNAL。
	Type ServiceBackendType `json:"type" gorm:"column:type;not null;default:'SVC';type:text"`
	// ArbitraryEndpoint 是一个可选的任意后端地址，仅当 Type 为 EXTERNAL 时使用。
	ArbitraryEndpoint string `json:"arbitrary_endpoint" gorm:"column:arbitrary_endpoint;not null;default:'';type:text"`
	// ServiceNamespace 是目标 Service 所在命名空间。
	ServiceNamespace string `json:"service_namespace" gorm:"column:service_namespace;not null;default:'';type:text"`
	// ServiceName 是目标 Service 名称。
	ServiceName string `json:"service_name" gorm:"column:service_name;not null;default:'';type:text"`
	// ServicePort 是目标 Service 端口。
	Port uint32 `json:"service_port" gorm:"column:service_port;not null;default:0"`
}

func (ServiceBackendRef) TableName() string {
	return "service_backend_refs"
}
