package dns

import (
	"context"
	"fmt"
)

// DeleteRecordInput 定义删除 DNS 记录时的输入参数。
type DeleteRecordInput struct {
	// DomainID 是待删除记录所属域名入口对象 ID。
	DomainID string
	// RecordID 是待删除记录的唯一标识。
	RecordID string
}

// Validate 校验删除 DNS 记录请求是否合法。
func (in DeleteRecordInput) Validate() error {
	if in.DomainID == "" {
		return fmt.Errorf("domain id is required")
	}
	if in.RecordID == "" {
		return fmt.Errorf("record id is required")
	}
	return nil
}

// DeleteRecord 删除一条 DNS 记录。
//
// 影响：
// - 从该域名的 DNS 记录集合中移除目标记录
// - 重新写回该域名的 DNS 物化结果
// - 返回变更影响摘要
func (e *Engine) DeleteRecord(_ context.Context, input DeleteRecordInput) (*ChangeEffect, error) {
	if err := input.Validate(); err != nil {
		return nil, err
	}

	deleted, err := e.store.Delete(input.DomainID, input.RecordID)
	if err != nil {
		return nil, err
	}

	newVersion := deleted.Version + 1

	return &ChangeEffect{
		DomainID:        input.DomainID,
		ZoneID:          deleted.ZoneID,
		FQDN:            deleted.FQDN,
		RecordType:      deleted.RecordType,
		Action:          "del",
		OldVersion:      deleted.Version,
		NewVersion:      newVersion,
		RecordsAffected: 1,
	}, nil
}
