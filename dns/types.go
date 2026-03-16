// Package dns 定义 DNS 查询解析、记录变更和执行引擎。
package dns

import (
	"strings"

	"github.com/jabberwocky238/luna-edge/repository/metadata"
	mdns "github.com/miekg/dns"
)

// RecordValue 表示一条 DNS 记录中的单个值。
type RecordValue string

// DNSQuestion 表示 DNS 运行时的最终查询主体。
//
// 它不是声明态对象，也不是数据库表，而是 slave/runtime 在解析请求时
// 真正面对的查询键：给定一个 FQDN 和记录类型，系统需要返回对应答案集。
type DNSQuestion struct {
	// FQDN 是规范化后的完整域名，统一使用带尾随点的小写形式。
	FQDN string
	// RecordType 是规范化后的记录类型，例如 A、AAAA、TXT。
	RecordType metadata.DNSRecordType
}

// DNSAnswerSet 表示一个 DNSQuestion 当前最终生效的答案集合。
//
// 它是 DNS 侧真正被查询和同步的运行时视图：
// `DNSQuestion -> DNSAnswerSet`。
// 前置的 Zone、DomainEndpoint、DNSRecord 都只是用于推导该结果的源数据或中间态。
type DNSAnswerSet struct {
	// Question 是该答案集对应的查询键。
	Question DNSQuestion
	// Found 表示当前是否命中至少一条可返回记录。
	Found bool
	// Records 是当前最终返回给客户端的记录集合。
	Records []metadata.DNSRecord
}

// ChangeEffect 描述某次 DNS 变更的影响结果。
type ChangeEffect struct {
	// DomainID 是受影响的域名入口对象 ID。
	DomainID string
	// ZoneID 是受影响的 Zone ID。
	ZoneID string
	// FQDN 是受影响的完整域名。
	FQDN string
	// RecordType 是受影响的记录类型。
	RecordType metadata.DNSRecordType
	// Action 是执行的动作，例如 add、mod、del。
	Action string
	// OldVersion 是变更前版本号。
	OldVersion uint64
	// NewVersion 是变更后版本号。
	NewVersion uint64
	// RecordsAffected 是本次操作影响的记录条数。
	RecordsAffected int
}

func normalizeFQDN(value string) string {
	value = strings.TrimSpace(strings.ToLower(value))
	value = strings.TrimSuffix(value, ".")
	if value == "" {
		return ""
	}
	return value + "."
}

func normalizeRecordType(value metadata.DNSRecordType) metadata.DNSRecordType {
	value = metadata.DNSRecordType(strings.ToUpper(strings.TrimSpace(string(value))))
	if value == "" {
		return ""
	}
	if _, ok := mdns.StringToType[string(value)]; ok {
		return value
	}
	return ""
}
