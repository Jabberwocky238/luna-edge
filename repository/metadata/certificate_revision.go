package metadata

import "time"

type CertificateRevisionStatus string

// CertificateRevisionStatus 表示单个证书版本的生命周期状态。
const (
	// CertificateRevisionStatusPending 表示证书版本已创建但尚未可用。
	CertificateRevisionStatusPending CertificateRevisionStatus = "pending"
	// CertificateRevisionStatusActive 表示该证书版本当前可用并可能已生效。
	CertificateRevisionStatusActive CertificateRevisionStatus = "active"
	// CertificateRevisionStatusFailed 表示该证书版本生成失败。
	CertificateRevisionStatusFailed CertificateRevisionStatus = "failed"
	// CertificateRevisionStatusRetired 表示该证书版本已退役不再使用。
	CertificateRevisionStatusRetired CertificateRevisionStatus = "retired"
)

type ChallengeType string

const (
	ChallengeTypeDNS01  ChallengeType = "dns-01"
	ChallengeTypeHTTP01 ChallengeType = "http-01"
)

// CertificateRevision 表示某个域名的一个证书版本元数据。
type CertificateRevision struct {
	// ID 是证书版本对象的唯一标识。
	ID string `json:"id" gorm:"column:id;primaryKey;type:varchar(64)"`
	// DomainID 是该证书版本所属的域名入口对象 ID。
	DomainID string `json:"domain_id" gorm:"column:domain_id;not null;index;type:varchar(64)"`
	// ZoneID 是该证书对应的 Zone ID。
	ZoneID string `json:"zone_id" gorm:"column:zone_id;not null;index;type:varchar(64)"`
	// Hostname 是该证书覆盖的完整域名。
	Hostname string `json:"hostname" gorm:"column:hostname;not null;index;type:varchar(255)"`
	// Revision 是按域名递增的证书版本号。
	Revision uint64 `json:"revision" gorm:"column:revision;not null"`
	// Version 是用于追踪和展示的版本字符串。
	Version string `json:"version" gorm:"column:version;not null;default:'';type:varchar(128)"`
	// Provider 是签发该证书的提供方。
	Provider string `json:"provider" gorm:"column:provider;not null;default:'';type:varchar(128)"`
	// ChallengeType 是申请该证书时使用的 challenge 类型。
	ChallengeType ChallengeType `json:"challenge_type" gorm:"column:challenge_type;not null;default:'';type:varchar(32)"`
	// ArtifactBucket 是对象存储中保存该证书的 bucket 名称。
	ArtifactBucket string `json:"artifact_bucket" gorm:"column:artifact_bucket;not null;default:'';type:varchar(255)"`
	// ArtifactPrefix 是对象存储中保存该证书的前缀路径。
	ArtifactPrefix string `json:"artifact_prefix" gorm:"column:artifact_prefix;not null;default:'';type:varchar(512)"`
	// SHA256Crt 是证书正文文件的 SHA-256 摘要。
	SHA256Crt string `json:"sha256_crt" gorm:"column:sha256_crt;not null;default:'';type:varchar(128)"`
	// SHA256Key 是私钥文件的 SHA-256 摘要。
	SHA256Key string `json:"sha256_key" gorm:"column:sha256_key;not null;default:'';type:varchar(128)"`
	// Status 是该证书版本当前的生命周期状态。
	Status CertificateRevisionStatus `json:"status" gorm:"column:status;not null;default:pending;type:varchar(32)"`
	// NotBefore 是证书生效时间。
	NotBefore time.Time `json:"not_before" gorm:"column:not_before"`
	// NotAfter 是证书过期时间。
	NotAfter time.Time `json:"not_after" gorm:"column:not_after"`
	// CreatedAt 是该证书版本创建时间。
	CreatedAt time.Time `json:"created_at" gorm:"column:created_at;not null;autoCreateTime"`
	// UpdatedAt 是该证书版本最后一次更新时间。
	UpdatedAt time.Time `json:"updated_at" gorm:"column:updated_at;not null;autoUpdateTime"`
}

func (CertificateRevision) TableName() string {
	return "certificate_revisions"
}
