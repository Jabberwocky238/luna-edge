package acme

import (
	"crypto"
	"crypto/ecdsa"
	"crypto/elliptic"
	"crypto/rand"
	"fmt"

	"github.com/go-acme/lego/v4/challenge"
	"github.com/go-acme/lego/v4/lego"
	"github.com/go-acme/lego/v4/registration"
	"github.com/jabberwocky238/luna-edge/repository/metadata"
)

type LegoIssuerFactory struct{}

func (LegoIssuerFactory) New(config IssuerConfig, challengeType metadata.ChallengeType, provider challenge.Provider) (CertificateIssuer, error) {
	privateKey, err := ecdsa.GenerateKey(elliptic.P256(), rand.Reader)
	if err != nil {
		return nil, err
	}
	user := &acmeUser{Email: config.Email, key: privateKey}
	clientConfig := lego.NewConfig(user)
	clientConfig.CADirURL = config.Directory
	client, err := lego.NewClient(clientConfig)
	if err != nil {
		return nil, err
	}
	if config.Provider == metadata.ProviderZeroSSL {
		reg, err := client.Registration.RegisterWithExternalAccountBinding(registration.RegisterEABOptions{
			TermsOfServiceAgreed: true,
			Kid:                  config.EABKID,
			HmacEncoded:          config.EABHMACKey,
		})
		if err != nil {
			return nil, err
		}
		user.Registration = reg
	} else {
		reg, err := client.Registration.Register(registration.RegisterOptions{TermsOfServiceAgreed: true})
		if err != nil {
			return nil, err
		}
		user.Registration = reg
	}
	switch config.Provider {
	case metadata.ProviderLetsEncrypt, metadata.ProviderZeroSSL:
	default:
		return nil, fmt.Errorf("unsupported acme provider %q", config.Provider)
	}
	if provider == nil {
		return nil, fmt.Errorf("challenge provider is required")
	}
	return &legoIssuer{client: client, provider: provider, challengeType: challengeType}, nil
}

type acmeUser struct {
	Email        string
	Registration *registration.Resource
	key          crypto.PrivateKey
}

func (u *acmeUser) GetEmail() string                        { return u.Email }
func (u *acmeUser) GetRegistration() *registration.Resource { return u.Registration }
func (u *acmeUser) GetPrivateKey() crypto.PrivateKey        { return u.key }
