package dns

import (
	"context"
	"fmt"

	"github.com/jabberwocky238/luna-edge/repository/metadata"
)

// AddRecordInput 定义新增 DNS 记录时的输入参数。
type AddRecordInput struct {
	// Record 是要新增的目标记录。
	Record metadata.DNSRecord
}

// Validate 校验新增 DNS 记录请求是否合法。
func (in AddRecordInput) Validate() error {
	if in.Record.ID == "" {
		return fmt.Errorf("record id is required")
	}
	if in.Record.DomainID == "" {
		return fmt.Errorf("domain id is required")
	}
	if normalizeFQDN(in.Record.FQDN) == "" {
		return fmt.Errorf("fqdn is required")
	}
	if normalizeRecordType(in.Record.RecordType) == "" {
		return fmt.Errorf("record type is required")
	}
	return nil
}

// AddRecord 新增一条 DNS 记录。
//
// 影响：
// - 向 dns_records 写入新的物化记录
// - 推进该记录的版本号
// - 返回这次变更对请求路径造成的影响摘要
func (e *Engine) AddRecord(_ context.Context, input AddRecordInput) (*ChangeEffect, error) {
	if err := input.Validate(); err != nil {
		return nil, err
	}

	record := input.Record
	record.FQDN = normalizeFQDN(record.FQDN)
	record.RecordType = normalizeRecordType(record.RecordType)
	if record.Version == 0 {
		record.Version = 1
	}
	answerSet := e.store.Add(record)

	return &ChangeEffect{
		DomainID:        record.DomainID,
		ZoneID:          record.ZoneID,
		FQDN:            answerSet.Question.FQDN,
		RecordType:      answerSet.Question.RecordType,
		Action:          "add",
		OldVersion:      0,
		NewVersion:      record.Version,
		RecordsAffected: 1,
	}, nil
}
