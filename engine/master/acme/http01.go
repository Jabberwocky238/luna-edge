package acme

import (
	"fmt"
	"log"
)

func (p *masterChallengeProvider) presentHTTP01(_ string, token, keyAuth string) error {
	if p.service.http01 == nil {
		return fmt.Errorf("http01 challenge store is required")
	}
	log.Printf("acme: http01 present hostname=%s order_id=%s token=%s path=%s", p.domain.Hostname, p.orderID, token, http01Path(token))
	p.service.http01.SetHTTP01Challenge(token, keyAuth)
	log.Printf("acme: http01 registered hostname=%s order_id=%s token=%s keyauth_len=%d", p.domain.Hostname, p.orderID, token, len(keyAuth))
	return nil
}

func (p *masterChallengeProvider) cleanupHTTP01(_ string, token, _ string) error {
	if p.service.http01 == nil {
		return nil
	}
	log.Printf("acme: http01 cleanup hostname=%s order_id=%s token=%s", p.domain.Hostname, p.orderID, token)
	p.service.http01.DeleteHTTP01Challenge(token)
	return nil
}

func http01Path(token string) string {
	return "/.well-known/acme-challenge/" + token
}
