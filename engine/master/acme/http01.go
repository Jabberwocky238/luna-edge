package acme

import (
	"fmt"
	"sync"

	"github.com/jabberwocky238/luna-edge/utils"
)

type http01Registry struct {
	mu    sync.RWMutex
	items map[string]string
}

func NewHTTP01Registry() *http01Registry {
	return &http01Registry{items: map[string]string{}}
}

func (r *http01Registry) Set(token, keyAuthorization string) {
	r.mu.Lock()
	defer r.mu.Unlock()
	r.items[token] = keyAuthorization
}

func (r *http01Registry) Get(token string) (string, bool) {
	r.mu.RLock()
	defer r.mu.RUnlock()
	value, ok := r.items[token]
	return value, ok
}

func (r *http01Registry) Delete(token string) {
	r.mu.Lock()
	defer r.mu.Unlock()
	delete(r.items, token)
}

func (p *masterChallengeProvider) presentHTTP01(_ string, token, keyAuth string) error {
	if p.service.http01 == nil {
		return fmt.Errorf("http01 challenge store is required")
	}
	utils.CertLogf("acme: http01 present hostname=%s order_id=%s token=%s path=%s", p.domain.Hostname, p.orderID, token, http01Path(token))
	p.service.http01.Set(token, keyAuth)
	utils.CertLogf("acme: http01 registered hostname=%s order_id=%s token=%s keyauth_len=%d", p.domain.Hostname, p.orderID, token, len(keyAuth))
	return nil
}

func (p *masterChallengeProvider) cleanupHTTP01(_ string, token, _ string) error {
	if p.service.http01 == nil {
		return nil
	}
	utils.CertLogf("acme: http01 cleanup hostname=%s order_id=%s token=%s", p.domain.Hostname, p.orderID, token)
	p.service.http01.Delete(token)
	return nil
}

func http01Path(token string) string {
	return "/.well-known/acme-challenge/" + token
}
