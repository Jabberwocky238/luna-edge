package acme

import (
	"fmt"
)

func (p *masterChallengeProvider) presentHTTP01(_ string, token, keyAuth string) error {
	if p.service.http01 == nil {
		return fmt.Errorf("http01 challenge store is required")
	}
	p.service.http01.SetHTTP01Challenge(token, keyAuth)
	return nil
}

func (p *masterChallengeProvider) cleanupHTTP01(_ string, token, _ string) error {
	if p.service.http01 == nil {
		return nil
	}
	p.service.http01.DeleteHTTP01Challenge(token)
	return nil
}

func http01Path(token string) string {
	return "/.well-known/acme-challenge/" + token
}
