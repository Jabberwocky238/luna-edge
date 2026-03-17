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
	certLogf("acme: dns01 present hostname=%s order_id=%s token=%s fqdn=%s", p.domain.Hostname, p.orderID, token, info.EffectiveFQDN)
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
		certLogf("acme: dns01 persist failed hostname=%s token=%s record_id=%s err=%v", p.domain.Hostname, token, record.ID, err)
		return err
	}
	certLogf("acme: dns01 persisted hostname=%s token=%s record_id=%s", p.domain.Hostname, token, record.ID)
	return p.service.publishChange(ctx, p.domain.Hostname)
}

func (p *masterChallengeProvider) cleanupDNS01(_ string, token, _ string) error {
	ctx := context.Background()
	certLogf("acme: dns01 cleanup hostname=%s order_id=%s token=%s record_id=%s", p.domain.Hostname, p.orderID, token, p.dns01RecordID(token))
	if err := p.service.repo.DNSRecords().DeleteResourceByField(ctx, &metadata.DNSRecord{}, "id", p.dns01RecordID(token)); err != nil {
		certLogf("acme: dns01 cleanup failed hostname=%s token=%s err=%v", p.domain.Hostname, token, err)
		return err
	}
	return p.service.publishChange(ctx, p.domain.Hostname)
}

func (p *masterChallengeProvider) dns01RecordID(token string) string {
	return "acme-dns01-" + p.domain.ID + "-" + token
}
