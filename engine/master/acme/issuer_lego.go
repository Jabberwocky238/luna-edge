package acme

import (
	"context"
	"fmt"

	"github.com/go-acme/lego/v4/certificate"
	"github.com/go-acme/lego/v4/challenge"
	"github.com/go-acme/lego/v4/lego"
	"github.com/jabberwocky238/luna-edge/repository/metadata"
)

type legoIssuer struct {
	client        *lego.Client
	provider      challenge.Provider
	challengeType metadata.ChallengeType
}

func (i *legoIssuer) Obtain(_ context.Context, domains []string) (*certificate.Resource, error) {
	if len(domains) == 0 {
		return nil, fmt.Errorf("domains are required")
	}
	switch i.challengeType {
	case metadata.ChallengeTypeDNS01:
		if timeoutProvider, ok := i.provider.(challenge.ProviderTimeout); ok {
			if err := i.client.Challenge.SetDNS01Provider(timeoutProvider); err != nil {
				return nil, err
			}
		} else {
			if err := i.client.Challenge.SetDNS01Provider(i.provider); err != nil {
				return nil, err
			}
		}
	case metadata.ChallengeTypeHTTP01:
		if err := i.client.Challenge.SetHTTP01Provider(i.provider); err != nil {
			return nil, err
		}
	default:
		return nil, fmt.Errorf("unsupported challenge type %q", i.challengeType)
	}
	return i.client.Certificate.Obtain(certificate.ObtainRequest{
		Domains: domains,
		Bundle:  true,
	})
}
