package dns

import (
	"context"
	"fmt"

	"github.com/jabberwocky238/luna-edge/repository/metadata"
)

// ModifyRecordInput 定义修改 DNS 记录时的输入参数。
type ModifyRecordInput struct {
	// DomainID 是待修改记录所属域名入口对象 ID。
	DomainID string
	// RecordID 是待修改记录的唯一标识。
	RecordID string
	// FQDN 是待修改记录的完整域名。
	FQDN string
	// RecordType 是待修改记录的类型。
	RecordType string
	// TTLSeconds 是修改后的 TTL。
	TTLSeconds uint32
	// ValuesJSON 是修改后的记录值 JSON。
	ValuesJSON string
	// Enabled 表示修改后是否启用该记录。
	Enabled bool
}

// Validate 校验修改 DNS 记录请求是否合法。
func (in ModifyRecordInput) Validate() error {
	if in.DomainID == "" {
		return fmt.Errorf("domain id is required")
	}
	if in.RecordID == "" {
		return fmt.Errorf("record id is required")
	}
	if normalizeFQDN(in.FQDN) == "" {
		return fmt.Errorf("fqdn is required")
	}
	if normalizeRecordType(in.RecordType) == "" {
		return fmt.Errorf("record type is required")
	}
	return nil
}

// ModifyRecord 修改一条 DNS 记录。
//
// 影响：
// - 修改指定记录的 TTL、值集合或启用状态
// - 递增该记录的版本号
// - 返回变更影响摘要
func (e *Engine) ModifyRecord(_ context.Context, input ModifyRecordInput) (*ChangeEffect, error) {
	if err := input.Validate(); err != nil {
		return nil, err
	}

	updated, err := e.store.Modify(input.DomainID, input.RecordID, func(record *metadata.DNSRecord) error {
		record.FQDN = normalizeFQDN(input.FQDN)
		record.RecordType = normalizeRecordType(input.RecordType)
		record.TTLSeconds = input.TTLSeconds
		record.ValuesJSON = input.ValuesJSON
		record.Enabled = input.Enabled
		record.Version++
		return nil
	})
	if err != nil {
		return nil, err
	}

	return &ChangeEffect{
		DomainID:        input.DomainID,
		ZoneID:          updated.ZoneID,
		FQDN:            normalizeFQDN(input.FQDN),
		RecordType:      normalizeRecordType(input.RecordType),
		Action:          "mod",
		OldVersion:      updated.Version - 1,
		NewVersion:      updated.Version,
		RecordsAffected: 1,
	}, nil
}
