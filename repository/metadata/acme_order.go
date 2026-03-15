package metadata

import "time"

// ACMEOrderStatus 表示 ACME 订单的生命周期状态。
const (
	// ACMEOrderStatusPending 表示订单已创建但尚未进入可完成阶段。
	ACMEOrderStatusPending    = "pending"
	// ACMEOrderStatusReady 表示订单已满足 finalize 前置条件。
	ACMEOrderStatusReady      = "ready"
	// ACMEOrderStatusProcessing 表示订单正在由 ACME 服务端处理。
	ACMEOrderStatusProcessing = "processing"
	// ACMEOrderStatusValid 表示订单已完成并签发成功。
	ACMEOrderStatusValid      = "valid"
	// ACMEOrderStatusInvalid 表示订单失败或被判定无效。
	ACMEOrderStatusInvalid    = "invalid"
)

// ACMEOrder 表示一次证书签发对应的 ACME 订单元数据。
type ACMEOrder struct {
	// ID 是 ACME 订单对象的唯一标识。
	ID                    string    `json:"id" gorm:"column:id;primaryKey;type:varchar(64)"`
	// DomainID 是该订单所属的域名入口对象 ID。
	DomainID              string    `json:"domain_id" gorm:"column:domain_id;not null;index;type:varchar(64)"`
	// CertificateRevisionID 是该订单目标证书版本的唯一标识。
	CertificateRevisionID string    `json:"certificate_revision_id" gorm:"column:certificate_revision_id;not null;index;type:varchar(64)"`
	// Provider 是本次签发使用的 ACME 提供方名称。
	Provider              string    `json:"provider" gorm:"column:provider;not null;default:'';type:varchar(128)"`
	// AccountRef 是本次订单使用的 ACME 账户引用。
	AccountRef            string    `json:"account_ref" gorm:"column:account_ref;not null;default:'';type:varchar(255)"`
	// OrderRef 是 ACME 服务端返回的订单引用或 URL。
	OrderRef              string    `json:"order_ref" gorm:"column:order_ref;not null;default:'';type:varchar(512)"`
	// Status 是该订单当前所处的生命周期状态。
	Status                string    `json:"status" gorm:"column:status;not null;default:pending;type:varchar(32)"`
	// ErrorMessage 记录该订单最近一次失败原因。
	ErrorMessage          string    `json:"error_message" gorm:"column:error_message;not null;default:'';type:text"`
	// StartedAt 是订单开始处理的时间。
	StartedAt             time.Time `json:"started_at" gorm:"column:started_at;not null;autoCreateTime"`
	// CompletedAt 是订单处理完成的时间。
	CompletedAt           time.Time `json:"completed_at" gorm:"column:completed_at"`
	// UpdatedAt 是订单记录最后一次更新时间。
	UpdatedAt             time.Time `json:"updated_at" gorm:"column:updated_at;not null;autoUpdateTime"`
}

func (ACMEOrder) TableName() string {
	return "acme_orders"
}
