package acme

import (
	"context"
	"encoding/json"

	"github.com/go-acme/lego/v4/challenge/dns01"
	"github.com/jabberwocky238/luna-edge/repository/metadata"
)

func (p *masterChallengeProvider) presentDNS01(domain, token, keyAuth string) error {
	ctx := context.Background()
	info := dns01.GetChallengeInfo(domain, keyAuth)
	values, _ := json.Marshal([]string{info.Value})
	record := &metadata.DNSRecord{
		ID:           p.dns01RecordID(token),
		FQDN:         info.EffectiveFQDN,
		RecordType:   metadata.DNSTypeTXT,
		TTLSeconds:   p.service.cfg.DNS01TTL,
		ValuesJSON:   string(values),
		RoutingClass: metadata.RoutingClassFirst,
		Enabled:      true,
	}
	if err := p.service.repo.DNSRecords().UpsertResource(ctx, record); err != nil {
		return err
	}
	return p.service.publishChange(ctx)
}

func (p *masterChallengeProvider) cleanupDNS01(_ string, token, _ string) error {
	ctx := context.Background()
	if err := p.service.repo.DNSRecords().DeleteResourceByField(ctx, &metadata.DNSRecord{}, "id", p.dns01RecordID(token)); err != nil {
		return err
	}
	return p.service.publishChange(ctx)
}

func (p *masterChallengeProvider) dns01RecordID(token string) string {
	return "acme-dns01-" + p.domain.ID + "-" + token
}
