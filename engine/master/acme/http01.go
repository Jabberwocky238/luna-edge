package acme

import (
	"context"

	"github.com/jabberwocky238/luna-edge/repository/metadata"
)

func (p *masterChallengeProvider) presentHTTP01(_ string, token, keyAuth string) error {
	ctx := context.Background()
	backendID := p.http01BackendID(token)
	backend := &metadata.ServiceBackendRef{
		ID:               backendID,
		ServiceNamespace: "acme",
		ServiceName:      "http01",
		ServicePort:      8089,
	}
	_ = keyAuth
	if err := p.service.repo.ServiceBindingRefs().UpsertResource(ctx, backend); err != nil {
		return err
	}
	route := &metadata.HTTPRoute{
		ID:               p.http01RouteID(token),
		DomainEndpointID: p.domain.ID,
		Hostname:         p.domain.Hostname,
		Path:             http01Path(token),
		Priority:         p.service.cfg.HTTP01Priority,
		BackendRefID:     backendID,
	}
	if err := p.service.repo.HTTPRoutes().UpsertResource(ctx, route); err != nil {
		return err
	}
	return p.service.publishChange(ctx)
}

func (p *masterChallengeProvider) cleanupHTTP01(_ string, token, _ string) error {
	ctx := context.Background()
	if err := p.service.repo.HTTPRoutes().DeleteResourceByField(ctx, &metadata.HTTPRoute{}, "id", p.http01RouteID(token)); err != nil {
		return err
	}
	if err := p.service.repo.ServiceBindingRefs().DeleteResourceByField(ctx, &metadata.ServiceBackendRef{}, "id", p.http01BackendID(token)); err != nil {
		return err
	}
	return p.service.publishChange(ctx)
}

func (p *masterChallengeProvider) http01RouteID(token string) string {
	return "acme-http01-route-" + p.domain.ID + "-" + token
}

func (p *masterChallengeProvider) http01BackendID(token string) string {
	return "acme-http01-backend-" + p.domain.ID + "-" + token
}

func http01Path(token string) string {
	return "/.well-known/acme-challenge/" + token
}
