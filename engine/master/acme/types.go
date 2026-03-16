package acme

import (
	"context"
	"time"

	"github.com/go-acme/lego/v4/certificate"
	"github.com/go-acme/lego/v4/challenge"
	enginepkg "github.com/jabberwocky238/luna-edge/engine"
	"github.com/jabberwocky238/luna-edge/repository"
	"github.com/jabberwocky238/luna-edge/repository/metadata"
)

const (
	ProviderLetsEncrypt = "letsencrypt"
	ProviderZeroSSL     = "zerossl"

	zeroSSLDirectoryURL = "https://acme.zerossl.com/v2/DV90"
)

type Config struct {
	DefaultEmail          string
	DefaultArtifactBucket string
	ArtifactPrefix        string
	DNS01TTL              uint32
	HTTP01Priority        int32
	DNS01Timeout          time.Duration
	DNS01Interval         time.Duration
}

type IssueRequest struct {
	DomainID      string
	Provider      string
	ChallengeType metadata.ChallengeType
	Email         string
	EABKID        string
	EABHMACKey    string
}

type Service struct {
	cfg      Config
	repo     repository.Repository
	publish  publisher
	bundles  bundleStore
	issuers  IssuerFactory
	now      func() time.Time
	idSuffix func() string
}

type publisher interface {
	PublishNode(ctx context.Context, nodeID string) error
}

type bundleStore interface {
	PutCertificateBundle(ctx context.Context, hostname string, revision uint64, bundle *enginepkg.CertificateBundle) error
}

type IssuerFactory interface {
	New(config IssuerConfig, challengeType metadata.ChallengeType, provider challenge.Provider) (CertificateIssuer, error)
}

type CertificateIssuer interface {
	Obtain(ctx context.Context, domains []string) (*certificate.Resource, error)
}

type IssuerConfig struct {
	Provider   string
	Email      string
	Directory  string
	EABKID     string
	EABHMACKey string
}
