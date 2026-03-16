package acme

import (
	"context"
	"encoding/json"
	"fmt"
	"time"

	"github.com/go-acme/lego/v4/challenge/dns01"
	"github.com/jabberwocky238/luna-edge/repository/metadata"
)

type masterChallengeProvider struct {
	service       *Service
	domain        *metadata.DomainEndpoint
	orderID       string
	challengeType metadata.ChallengeType
	timeout       time.Duration
	interval      time.Duration
}

func (p *masterChallengeProvider) Present(domain, token, keyAuth string) error {
	ctx := context.Background()
	challengeID := "acmechal-" + p.service.idSuffix()
	now := p.service.now()
	switch p.challengeType {
	case metadata.ChallengeTypeDNS01:
		info := dns01.GetChallengeInfo(domain, keyAuth)
		values, _ := json.Marshal([]string{info.Value})
		record := &metadata.DNSRecord{
			ID:         "dnsrec-" + p.service.idSuffix(),
			ZoneID:     p.domain.ZoneID,
			DomainID:   p.domain.ID,
			FQDN:       info.EffectiveFQDN,
			RecordType: "TXT",
			TTLSeconds: p.service.cfg.DNS01TTL,
			ValuesJSON: string(values),
			Version:    uint64(now.UnixNano()),
		}
		if err := p.service.repo.DNSRecords().UpsertResource(ctx, record); err != nil {
			return err
		}
		chal := &metadata.ACMEChallenge{
			ID:                     challengeID,
			ACMEOrderID:            p.orderID,
			Identifier:             domain,
			Type:                   metadata.ChallengeTypeDNS01,
			Token:                  token,
			KeyAuthorizationDigest: info.Value,
			PresentationFQDN:       info.EffectiveFQDN,
			PresentationValue:      info.Value,
			Status:                 metadata.ACMEChallengeStatusPresented,
			PresentedAt:            now,
		}
		if err := p.service.repo.ACMEChallenges().UpsertResource(ctx, chal); err != nil {
			return err
		}
		_ = p.service.repo.DomainEndpointStatuses().UpsertResource(ctx, &metadata.DomainEndpointStatus{
			DomainEndpointID:    p.domain.ID,
			ObservedGeneration:  p.domain.Generation,
			Phase:               metadata.DomainPhaseChallenging,
			DNSReady:            true,
			ChallengeReady:      true,
			CertificateReady:    false,
			CertificateRevision: 0,
			Ready:               false,
			UpdatedAt:           now,
		})
	case metadata.ChallengeTypeHTTP01:
		bindingID := "acmebind-" + p.service.idSuffix()
		routeJSON, _ := json.Marshal(map[string]any{
			"kind":              "acme-http01",
			"path":              "/.well-known/acme-challenge/" + token,
			"priority":          p.service.cfg.HTTP01Priority,
			"key_authorization": keyAuth,
		})
		binding := &metadata.ServiceBinding{
			ID:           bindingID,
			DomainID:     p.domain.ID,
			Hostname:     p.domain.Hostname,
			ServiceID:    "acme-http01",
			Name:         "acme-http01",
			Protocol:     metadata.ServiceBindingRouteKindHTTP,
			RouteVersion: uint64(now.UnixNano()),
			BackendJSON:  string(routeJSON),
		}
		if err := p.service.repo.ServiceBindings().UpsertResource(ctx, binding); err != nil {
			return err
		}
		route := &metadata.HTTPRoute{
			ID:           "httproute-" + p.service.idSuffix(),
			DomainID:     p.domain.ID,
			Hostname:     p.domain.Hostname,
			Path:         "/.well-known/acme-challenge/" + token,
			Priority:     p.service.cfg.HTTP01Priority,
			BindingID:    bindingID,
			RouteVersion: binding.RouteVersion,
			RouteJSON:    string(routeJSON),
		}
		if err := p.service.repo.HTTPRoutes().UpsertResource(ctx, route); err != nil {
			return err
		}
		chal := &metadata.ACMEChallenge{
			ID:                     challengeID,
			ACMEOrderID:            p.orderID,
			Identifier:             domain,
			Type:                   metadata.ChallengeTypeHTTP01,
			Token:                  token,
			KeyAuthorizationDigest: keyAuth,
			PresentationFQDN:       p.domain.Hostname,
			PresentationValue:      keyAuth,
			Status:                 metadata.ACMEChallengeStatusPresented,
			PresentedAt:            now,
		}
		if err := p.service.repo.ACMEChallenges().UpsertResource(ctx, chal); err != nil {
			return err
		}
		_ = p.service.repo.DomainEndpointStatuses().UpsertResource(ctx, &metadata.DomainEndpointStatus{
			DomainEndpointID:    p.domain.ID,
			ObservedGeneration:  p.domain.Generation,
			Phase:               metadata.DomainPhaseChallenging,
			ChallengeReady:      true,
			RouteReady:          true,
			CertificateReady:    false,
			CertificateRevision: 0,
			Ready:               false,
			UpdatedAt:           now,
		})
	default:
		return fmt.Errorf("unsupported challenge type %q", p.challengeType)
	}
	return p.service.publishDomain(ctx, p.domain.ID)
}

func (p *masterChallengeProvider) CleanUp(domain, token, keyAuth string) error {
	ctx := context.Background()
	challenges, err := p.service.repo.ListACMEChallengesByOrderID(ctx, p.orderID)
	if err != nil {
		return err
	}
	now := p.service.now()
	for i := range challenges {
		if challenges[i].Token != token || challenges[i].Type != p.challengeType {
			continue
		}
		switch p.challengeType {
		case metadata.ChallengeTypeDNS01:
			records, listErr := p.service.repo.ListDNSRecordsByDomainID(ctx, p.domain.ID)
			if listErr == nil {
				for j := range records {
					if records[j].ID != "" && normalizeString(records[j].FQDN) == normalizeString(challenges[i].PresentationFQDN) && records[j].RecordType == "TXT" {
						_ = p.service.repo.DNSRecords().DeleteResourceByField(ctx, &metadata.DNSRecord{}, "id", records[j].ID)
					}
				}
			}
		case metadata.ChallengeTypeHTTP01:
			routes, listErr := p.service.repo.ListHTTPRoutesByDomainID(ctx, p.domain.ID)
			if listErr == nil {
				for j := range routes {
					if routes[j].Path == "/.well-known/acme-challenge/"+token {
						_ = p.service.repo.HTTPRoutes().DeleteResourceByField(ctx, &metadata.HTTPRoute{}, "id", routes[j].ID)
						_ = p.service.repo.ServiceBindings().DeleteResourceByField(ctx, &metadata.ServiceBinding{}, "id", routes[j].BindingID)
					}
				}
			}
		}
		challenges[i].ValidatedAt = now
		challenges[i].Status = metadata.ACMEChallengeStatusCleaned
		challenges[i].CleanedUpAt = now
		_ = p.service.repo.ACMEChallenges().UpsertResource(ctx, &challenges[i])
	}
	return p.service.publishDomain(ctx, p.domain.ID)
}

func (p *masterChallengeProvider) Timeout() (time.Duration, time.Duration) {
	return p.timeout, p.interval
}
