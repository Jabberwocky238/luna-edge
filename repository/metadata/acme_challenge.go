package metadata

import "time"

// ACMEChallengeStatus 表示单个 ACME challenge 的生命周期状态。
type ACMEChallengeStatus string

const (
	// ACMEChallengeStatusPending 表示 challenge 已创建但尚未开始呈现。
	ACMEChallengeStatusPending   ACMEChallengeStatus = "pending"
	// ACMEChallengeStatusPresented 表示 challenge 所需内容已经对外呈现。
	ACMEChallengeStatusPresented ACMEChallengeStatus = "presented"
	// ACMEChallengeStatusValid 表示 challenge 已被 ACME 服务端验证通过。
	ACMEChallengeStatusValid     ACMEChallengeStatus = "valid"
	// ACMEChallengeStatusCleaned 表示 challenge 的临时呈现内容已经被清理。
	ACMEChallengeStatusCleaned   ACMEChallengeStatus = "cleaned"
	// ACMEChallengeStatusInvalid 表示 challenge 校验失败或流程失效。
	ACMEChallengeStatusInvalid   ACMEChallengeStatus = "invalid"
)

// ACMEChallenge 表示一次 ACME challenge 的元数据记录。
type ACMEChallenge struct {
	// ID 是 challenge 对象的唯一标识。
	ID                     string              `json:"id" gorm:"column:id;primaryKey;type:varchar(64)"`
	// ACMEOrderID 关联所属的 ACME 订单。
	ACMEOrderID            string              `json:"acme_order_id" gorm:"column:acme_order_id;not null;index;type:varchar(64)"`
	// Identifier 是该 challenge 对应的标识，通常是待验证域名。
	Identifier             string              `json:"identifier" gorm:"column:identifier;not null;index;type:varchar(255)"`
	// Type 是 challenge 类型，例如 dns-01 或 http-01。
	Type                   ChallengeType              `json:"type" gorm:"column:type;not null;type:varchar(32)"`
	// Token 是 ACME 服务端分配的 challenge token。
	Token                  string              `json:"token" gorm:"column:token;not null;index;type:varchar(255)"`
	// KeyAuthorizationDigest 是 key authorization 的摘要或衍生值。
	KeyAuthorizationDigest string              `json:"key_authorization_digest" gorm:"column:key_authorization_digest;not null;default:'';type:varchar(255)"`
	// PresentationFQDN 是需要对外提供 challenge 内容的完整域名。
	PresentationFQDN       string              `json:"presentation_fqdn" gorm:"column:presentation_fqdn;not null;default:'';type:varchar(255)"`
	// PresentationValue 是实际对外呈现的 challenge 内容。
	PresentationValue      string              `json:"presentation_value" gorm:"column:presentation_value;not null;default:'';type:text"`
	// Status 是当前 challenge 的生命周期状态。
	Status                 ACMEChallengeStatus `json:"status" gorm:"column:status;not null;default:pending;type:varchar(32)"`
	// ErrorMessage 记录该 challenge 最近一次失败原因。
	ErrorMessage           string              `json:"error_message" gorm:"column:error_message;not null;default:'';type:text"`
	// PresentedAt 是 challenge 内容成功呈现的时间。
	PresentedAt            time.Time           `json:"presented_at" gorm:"column:presented_at"`
	// ValidatedAt 是 challenge 被验证通过的时间。
	ValidatedAt            time.Time           `json:"validated_at" gorm:"column:validated_at"`
	// CleanedUpAt 是 challenge 临时内容被清理完成的时间。
	CleanedUpAt            time.Time           `json:"cleaned_up_at" gorm:"column:cleaned_up_at"`
	// UpdatedAt 是该 challenge 记录最后一次更新时间。
	UpdatedAt              time.Time           `json:"updated_at" gorm:"column:updated_at;not null;autoUpdateTime"`
}

func (ACMEChallenge) TableName() string {
	return "acme_challenges"
}
