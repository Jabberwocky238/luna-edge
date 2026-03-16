package dns

import (
	"context"
	"fmt"
)

// Resolver 定义 DNS 查询解析能力。
type Resolver interface {
	// Lookup 按最终查询主体查找 DNS 答案集。
	Lookup(ctx context.Context, question DNSQuestion) (*DNSAnswerSet, error)
}

// Lookup 按最终查询主体查找 DNS 答案集。
func (e *Engine) Lookup(ctx context.Context, question DNSQuestion) (*DNSAnswerSet, error) {
	question = normalizeQuestion(question)
	if question.FQDN == "" {
		return nil, fmt.Errorf("fqdn is required")
	}
	if question.RecordType == "" {
		return nil, fmt.Errorf("record type is required")
	}
	if e.store == nil {
		return nil, fmt.Errorf("dns memory store is not initialized")
	}

	if e.k8sBridge != nil {
		if answerSet, ok := e.k8sBridge.Lookup(question); ok {
			return answerSet, nil
		}
	}
	if answerSet, ok := e.store.Lookup(question); ok {
		return answerSet, nil
	}
	if answerSet, err := e.forwarder.Lookup(ctx, question); err == nil && answerSet != nil && answerSet.Found {
		return answerSet, nil
	}
	return &DNSAnswerSet{Question: question}, nil
}
