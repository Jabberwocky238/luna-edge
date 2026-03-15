package metadata

import "time"

// Node 表示一个运行中的 master、slave 或 hybrid 节点。
type Node struct {
	// ID 是节点的唯一标识。
	ID               string    `json:"id" gorm:"column:id;primaryKey;type:varchar(64)"`
	// Role 表示节点角色，例如 master、slave、hybrid。
	Role             string    `json:"role" gorm:"column:role;not null;type:varchar(32)"`
	// ClusterID 表示节点所属集群的标识。
	ClusterID        string    `json:"cluster_id" gorm:"column:cluster_id;not null;index;type:varchar(64)"`
	// Region 表示节点所在地域。
	Region           string    `json:"region" gorm:"column:region;not null;default:'';index;type:varchar(64)"`
	// AvailabilityZone 表示节点所在可用区。
	AvailabilityZone string    `json:"availability_zone" gorm:"column:availability_zone;not null;default:'';type:varchar(64)"`
	// AdvertiseAddr 是节点对外通告的访问地址。
	AdvertiseAddr    string    `json:"advertise_addr" gorm:"column:advertise_addr;not null;default:'';type:varchar(255)"`
	// CapabilityJSON 是节点能力描述的 JSON 文本。
	CapabilityJSON   string    `json:"capability_json" gorm:"column:capability_json;not null;default:'';type:text"`
	// LabelsJSON 是节点附加标签的 JSON 文本。
	LabelsJSON       string    `json:"labels_json" gorm:"column:labels_json;not null;default:'';type:text"`
	// SoftwareVersion 是节点当前运行的软件版本号。
	SoftwareVersion  string    `json:"software_version" gorm:"column:software_version;not null;default:'';type:varchar(64)"`
	// Status 是节点当前状态，例如 ready、degraded、offline。
	Status           string    `json:"status" gorm:"column:status;not null;default:unknown;type:varchar(32)"`
	// LastSeenAt 是最近一次收到该节点心跳的时间。
	LastSeenAt       time.Time `json:"last_seen_at" gorm:"column:last_seen_at"`
	// CreatedAt 是节点记录创建时间。
	CreatedAt        time.Time `json:"created_at" gorm:"column:created_at;not null;autoCreateTime"`
	// UpdatedAt 是节点记录最后一次更新时间。
	UpdatedAt        time.Time `json:"updated_at" gorm:"column:updated_at;not null;autoUpdateTime"`
}

func (Node) TableName() string {
	return "nodes"
}
