package metadata

import "time"

// DomainPhase 表示域名入口对象的收敛阶段。
const (
	// DomainPhasePending 表示对象已创建但尚未开始完整收敛。
	DomainPhasePending     = "pending"
	// DomainPhaseAttaching 表示正在进行节点挂载分配。
	DomainPhaseAttaching   = "attaching"
	// DomainPhaseChallenging 表示正在处理 ACME challenge。
	DomainPhaseChallenging = "challenging"
	// DomainPhaseIssuing 表示正在签发或续期证书。
	DomainPhaseIssuing     = "issuing"
	// DomainPhasePropagating 表示正在向节点传播 DNS、路由或证书结果。
	DomainPhasePropagating = "propagating"
	// DomainPhaseReady 表示对象整体已经就绪。
	DomainPhaseReady       = "ready"
	// DomainPhaseDegraded 表示对象部分可用但存在降级。
	DomainPhaseDegraded    = "degraded"
	// DomainPhaseDeleting 表示对象正在删除。
	DomainPhaseDeleting    = "deleting"
	// DomainPhaseError 表示对象当前处于错误状态。
	DomainPhaseError       = "error"
)

// DomainEndpointStatus 表示 DomainEndpoint 的当前收敛状态。
type DomainEndpointStatus struct {
	// DomainEndpointID 是对应 DomainEndpoint 的 ID。
	DomainEndpointID    string    `json:"domain_endpoint_id" gorm:"column:domain_endpoint_id;primaryKey;type:varchar(64)"`
	// ObservedGeneration 是控制器已经处理到的期望态版本号。
	ObservedGeneration  uint64    `json:"observed_generation" gorm:"column:observed_generation;not null;default:0"`
	// Phase 是当前总体收敛阶段。
	Phase               string    `json:"phase" gorm:"column:phase;not null;default:pending;type:varchar(32)"`
	// DNSReady 表示 DNS 物化是否已经就绪。
	DNSReady            bool      `json:"dns_ready" gorm:"column:dns_ready;not null;default:false"`
	// ChallengeReady 表示 challenge 是否已经准备完成。
	ChallengeReady      bool      `json:"challenge_ready" gorm:"column:challenge_ready;not null;default:false"`
	// CertificateReady 表示证书是否已经签发完成。
	CertificateReady    bool      `json:"certificate_ready" gorm:"column:certificate_ready;not null;default:false"`
	// CertificateRevision 是当前生效证书的 revision 号。
	CertificateRevision uint64    `json:"certificate_revision" gorm:"column:certificate_revision;not null;default:0"`
	// RouteReady 表示入口路由是否已经生效。
	RouteReady          bool      `json:"route_ready" gorm:"column:route_ready;not null;default:false"`
	// AttachmentReady 表示节点挂载是否全部完成。
	AttachmentReady     bool      `json:"attachment_ready" gorm:"column:attachment_ready;not null;default:false"`
	// Ready 表示整个域名入口是否整体可用。
	Ready               bool      `json:"ready" gorm:"column:ready;not null;default:false"`
	// LastError 记录最近一次错误信息。
	LastError           string    `json:"last_error" gorm:"column:last_error;not null;default:'';type:text"`
	// LastErrorAt 记录最近一次错误发生时间。
	LastErrorAt         time.Time `json:"last_error_at" gorm:"column:last_error_at"`
	// UpdatedAt 是状态最后一次更新时间。
	UpdatedAt           time.Time `json:"updated_at" gorm:"column:updated_at;not null;autoUpdateTime"`
}

func (DomainEndpointStatus) TableName() string {
	return "domain_endpoint_status"
}
