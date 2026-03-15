// Package dns 定义 DNS 查询解析、记录变更和执行引擎。
package dns

// RecordValue 表示一条 DNS 记录中的单个值。
type RecordValue string

// ChangeEffect 描述某次 DNS 变更的影响结果。
type ChangeEffect struct {
	// DomainID 是受影响的域名入口对象 ID。
	DomainID string
	// ZoneID 是受影响的 Zone ID。
	ZoneID string
	// FQDN 是受影响的完整域名。
	FQDN string
	// RecordType 是受影响的记录类型。
	RecordType string
	// Action 是执行的动作，例如 add、mod、del。
	Action string
	// OldVersion 是变更前版本号。
	OldVersion uint64
	// NewVersion 是变更后版本号。
	NewVersion uint64
	// RecordsAffected 是本次操作影响的记录条数。
	RecordsAffected int
}
