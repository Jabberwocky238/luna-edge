package metadata

import "time"

type ChallengeType string

const (
	ChallengeTypeDNS01  ChallengeType = "dns-01"
	ChallengeTypeHTTP01 ChallengeType = "http-01"
)

// CertificateRevision 表示某个域名的一个证书版本元数据。
type CertificateRevision struct {
	Shared
	// ID 是证书版本对象的唯一标识。
	ID string `json:"id" gorm:"column:id;primaryKey;type:varchar(64)"`
	// DomainEndpointID 是该证书版本所属的域名入口对象 ID。
	DomainEndpointID string `json:"domain_endpoint_id" gorm:"column:domain_endpoint_id;not null;index;type:varchar(64)"`
	// Hostname 是该证书覆盖的完整域名。
	Hostname string `json:"hostname" gorm:"column:hostname;not null;index;type:varchar(255)"`
	// Revision 是按域名递增的证书版本号。
	Revision uint64 `json:"revision" gorm:"column:revision;not null"`
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
	// NotBefore 是证书生效时间。
	NotBefore time.Time `json:"not_before" gorm:"column:not_before"`
	// NotAfter 是证书过期时间。
	NotAfter time.Time `json:"not_after" gorm:"column:not_after"`
}

func (CertificateRevision) TableName() string {
	return "certificate_revisions"
}
