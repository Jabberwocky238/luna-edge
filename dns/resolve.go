package dns

import (
	"context"
	"fmt"
	"strings"

	"github.com/jabberwocky238/luna-edge/repository/metadata"
	mdns "github.com/miekg/dns"
)

// ResolveResult 表示一次 DNS 查询解析后的标准结果。
type ResolveResult struct {
	// FQDN 是查询的完整域名。
	FQDN string
	// RecordType 是查询的记录类型。
	RecordType string
	// Found 表示是否找到匹配记录。
	Found bool
	// Records 是命中的 DNS 记录集合。
	Records []metadata.DNSRecord
}

// Resolver 定义 DNS 查询解析能力。
type Resolver interface {
	// Resolve 解析指定域名和记录类型。
	Resolve(ctx context.Context, fqdn, recordType string) (*ResolveResult, error)
}

// Resolve 解析任意一类 DNS 查询。
//
// 当前实现支持对所有已物化的记录类型统一解析：
// - A
// - AAAA
// - CNAME
// - TXT
// - MX
// - NS
// - SRV
// - CAA
func (e *Engine) Resolve(ctx context.Context, fqdn, recordType string) (*ResolveResult, error) {
	fqdn = normalizeFQDN(fqdn)
	recordType = normalizeRecordType(recordType)
	if fqdn == "" {
		return nil, fmt.Errorf("fqdn is required")
	}
	if recordType == "" {
		return nil, fmt.Errorf("record type is required")
	}
	if e.store == nil {
		return nil, fmt.Errorf("dns memory store is not initialized")
	}

	result := &ResolveResult{
		FQDN:       fqdn,
		RecordType: recordType,
		Found:      false,
		Records:    []metadata.DNSRecord{},
	}

	if records, ok := e.store.Lookup(fqdn, recordType); ok {
		result.Records = append(result.Records, records...)
		result.Found = len(result.Records) > 0
		return result, nil
	}
	result.Found = len(result.Records) > 0
	return result, nil
}

func normalizeFQDN(value string) string {
	value = strings.TrimSpace(strings.ToLower(value))
	value = strings.TrimSuffix(value, ".")
	if value == "" {
		return ""
	}
	return value + "."
}

func normalizeRecordType(value string) string {
	value = strings.ToUpper(strings.TrimSpace(value))
	if value == "" {
		return ""
	}
	if _, ok := mdns.StringToType[value]; ok {
		return value
	}
	return ""
}
