package metadata

import "time"

// AttachmentState 表示域名在某个节点上的挂载状态。
const (
	// AttachmentStatePending 表示挂载已经创建但尚未开始应用。
	AttachmentStatePending  = "pending"
	// AttachmentStateApplying 表示节点正在应用 DNS、路由或证书变更。
	AttachmentStateApplying = "applying"
	// AttachmentStateReady 表示节点上的挂载已经生效。
	AttachmentStateReady    = "ready"
	// AttachmentStateStale 表示节点状态落后于期望版本。
	AttachmentStateStale    = "stale"
	// AttachmentStateError 表示挂载过程中发生错误。
	AttachmentStateError    = "error"
	// AttachmentStateDetached 表示该挂载已被解除。
	AttachmentStateDetached = "detached"
)

// Attachment 表示某个域名入口对象在某个节点上的期望和观测挂载状态。
type Attachment struct {
	// ID 是挂载记录的唯一标识。
	ID                         string    `json:"id" gorm:"column:id;primaryKey;type:varchar(64)"`
	// DomainID 是被挂载域名入口对象的 ID。
	DomainID                   string    `json:"domain_id" gorm:"column:domain_id;not null;index;type:varchar(64)"`
	// NodeID 是承载该域名的节点 ID。
	NodeID                     string    `json:"node_id" gorm:"column:node_id;not null;index;type:varchar(64)"`
	// Listener 表示节点内具体的监听器或入口实例名称。
	Listener                   string    `json:"listener" gorm:"column:listener;not null;default:'';type:varchar(128)"`
	// DesiredCertificateRevision 是 master 期望节点加载的证书版本号。
	DesiredCertificateRevision uint64    `json:"desired_certificate_revision" gorm:"column:desired_certificate_revision;not null;default:0"`
	// ObservedCertificateRevision 是节点实际上报已加载的证书版本号。
	ObservedCertificateRevision uint64   `json:"observed_certificate_revision" gorm:"column:observed_certificate_revision;not null;default:0"`
	// DesiredRouteVersion 是 master 期望节点应用的路由版本号。
	DesiredRouteVersion        uint64    `json:"desired_route_version" gorm:"column:desired_route_version;not null;default:0"`
	// ObservedRouteVersion 是节点实际上报已应用的路由版本号。
	ObservedRouteVersion       uint64    `json:"observed_route_version" gorm:"column:observed_route_version;not null;default:0"`
	// DesiredDNSVersion 是 master 期望节点应用的 DNS 物化版本号。
	DesiredDNSVersion          uint64    `json:"desired_dns_version" gorm:"column:desired_dns_version;not null;default:0"`
	// ObservedDNSVersion 是节点实际上报已应用的 DNS 物化版本号。
	ObservedDNSVersion         uint64    `json:"observed_dns_version" gorm:"column:observed_dns_version;not null;default:0"`
	// State 是该挂载当前总体状态。
	State                      string    `json:"state" gorm:"column:state;not null;default:pending;type:varchar(32)"`
	// LastError 记录该挂载最近一次错误信息。
	LastError                  string    `json:"last_error" gorm:"column:last_error;not null;default:'';type:text"`
	// LastReportAt 是节点最近一次上报该挂载状态的时间。
	LastReportAt               time.Time `json:"last_report_at" gorm:"column:last_report_at"`
	// UpdatedAt 是该挂载记录最后一次更新时间。
	UpdatedAt                  time.Time `json:"updated_at" gorm:"column:updated_at;not null;autoUpdateTime"`
}

func (Attachment) TableName() string {
	return "attachments"
}
