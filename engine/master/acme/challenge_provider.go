package acme

import (
	"fmt"
	"time"

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
	switch p.challengeType {
	case metadata.ChallengeTypeDNS01:
		return p.presentDNS01(domain, token, keyAuth)
	case metadata.ChallengeTypeHTTP01:
		return p.presentHTTP01(domain, token, keyAuth)
	default:
		return fmt.Errorf("unsupported challenge type %q", p.challengeType)
	}
}

func (p *masterChallengeProvider) CleanUp(domain, token, keyAuth string) error {
	switch p.challengeType {
	case metadata.ChallengeTypeDNS01:
		return p.cleanupDNS01(domain, token, keyAuth)
	case metadata.ChallengeTypeHTTP01:
		return p.cleanupHTTP01(domain, token, keyAuth)
	default:
		return fmt.Errorf("unsupported challenge type %q", p.challengeType)
	}
}

func (p *masterChallengeProvider) Timeout() (time.Duration, time.Duration) {
	return p.timeout, p.interval
}
