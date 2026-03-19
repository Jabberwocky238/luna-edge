package acme

import (
	"context"
	"fmt"
	"strings"
	"time"

	"github.com/go-acme/lego/v4/certificate"
	"github.com/go-acme/lego/v4/challenge"
	"github.com/go-acme/lego/v4/challenge/dns01"
	"github.com/go-acme/lego/v4/lego"
	"github.com/jabberwocky238/luna-edge/replication"
	"github.com/jabberwocky238/luna-edge/repository"
	"github.com/jabberwocky238/luna-edge/repository/metadata"
	"github.com/jabberwocky238/luna-edge/utils"
)

const (
	zeroSSLDirectoryURL = "https://acme.zerossl.com/v2/DV90"
)

type Config struct {
	DefaultProvider       metadata.ACMEProvider
	DefaultEmail          string
	DefaultEABKID         string
	DefaultEABHMACKey     string
	DefaultArtifactBucket string
	ArtifactPrefix        string
	DNS01TTL              uint32
	HTTP01Priority        int32
	DNS01Timeout          time.Duration
	DNS01Interval         time.Duration
}

type IssueRequest struct {
	Hostname      string
	Provider      metadata.ACMEProvider
	ChallengeType metadata.ChallengeType
	Email         string
	EABKID        string
	EABHMACKey    string
}

type Service struct {
	cfg      Config
	repo     repository.Repository
	notifier notifyHandler
	bundles  bundleStore
	issuers  IssuerFactory
	http01   *http01Registry
	now      func() time.Time
	idSuffix func() string
}

type notifyHandler = func(ctx context.Context, hostname string) error

type bundleStore interface {
	PutCertificateBundle(ctx context.Context, bundle *replication.CertificateBundle) error
}

type IssuerFactory interface {
	New(config IssuerConfig, challengeType metadata.ChallengeType, provider challenge.Provider) (CertificateIssuer, error)
}

type CertificateIssuer interface {
	Obtain(ctx context.Context, domains []string) (*certificate.Resource, error)
}

type IssuerConfig struct {
	Provider   metadata.ACMEProvider
	Email      string
	Directory  string
	EABKID     string
	EABHMACKey string
}

func NewService(cfg Config, repo repository.Repository, notifier notifyHandler, bundles bundleStore, issuer IssuerFactory, http01 *http01Registry) *Service {
	if cfg.DefaultProvider == "" {
		cfg.DefaultProvider = metadata.ProviderLetsEncrypt
	}
	if cfg.DNS01TTL == 0 {
		cfg.DNS01TTL = 60
	}
	if cfg.HTTP01Priority == 0 {
		cfg.HTTP01Priority = 100000
	}
	if cfg.DNS01Timeout <= 0 {
		cfg.DNS01Timeout = dns01.DefaultPropagationTimeout
	}
	if cfg.DNS01Interval <= 0 {
		cfg.DNS01Interval = dns01.DefaultPollingInterval
	}
	return &Service{
		cfg:      cfg,
		repo:     repo,
		notifier: notifier,
		bundles:  bundles,
		issuers:  issuer,
		http01:   http01,
		now:      func() time.Time { return time.Now().UTC() },
		idSuffix: randomID,
	}
}

func (s *Service) IssueCertificate(ctx context.Context, req IssueRequest) (*metadata.CertificateRevision, error) {
	if s == nil || s.repo == nil {
		return nil, fmt.Errorf("acme service repository is required")
	}
	utils.CertLogf("acme: issue requested  provider=%s challenge=%s", req.Hostname, req.Provider, req.ChallengeType)
	domain, err := s.repo.GetDomainEndpointByHostname(ctx, req.Hostname)
	if err != nil {
		utils.CertLogf("acme: load domain failed  err=%v", req.Hostname, err)
		return nil, err
	}
	if domain == nil {
		return nil, fmt.Errorf("domain endpoint %q not found", req.Hostname)
	}
	utils.CertLogf("acme: resolved domain hostname=%s backend_type=%s", domain.Hostname, domain.BackendType)

	issuerCfg, err := s.resolveIssuerConfig(req)
	if err != nil {
		utils.CertLogf("acme: resolve issuer config failed hostname=%s err=%v", domain.Hostname, err)
		return nil, err
	}
	utils.CertLogf("acme: issuer config hostname=%s provider=%s directory=%s email=%s", domain.Hostname, issuerCfg.Provider, issuerCfg.Directory, issuerCfg.Email)
	revisionNumber, err := s.nextRevision(ctx, domain.Hostname)
	if err != nil {
		utils.CertLogf("acme: next revision failed err=%v", err)
		return nil, err
	}
	solver := &masterChallengeProvider{
		service:       s,
		domain:        domain,
		orderID:       "acmeorder-" + s.idSuffix(),
		challengeType: req.ChallengeType,
		timeout:       s.cfg.DNS01Timeout,
		interval:      s.cfg.DNS01Interval,
	}
	issuer, err := s.issuers.New(issuerCfg, req.ChallengeType, solver)
	if err != nil {
		utils.CertLogf("acme: create issuer failed hostname=%s provider=%s challenge=%s err=%v", domain.Hostname, issuerCfg.Provider, req.ChallengeType, err)
		return nil, err
	}
	utils.CertLogf("acme: issuer created hostname=%s provider=%s challenge=%s order_id=%s", domain.Hostname, issuerCfg.Provider, req.ChallengeType, solver.orderID)

	resource, err := issuer.Obtain(ctx, []string{domain.Hostname})
	if err != nil {
		utils.CertLogf("acme: obtain certificate failed hostname=%s provider=%s challenge=%s err=%v", domain.Hostname, issuerCfg.Provider, req.ChallengeType, err)
		return nil, err
	}
	utils.CertLogf("acme: obtain certificate succeeded hostname=%s provider=%s challenge=%s", domain.Hostname, issuerCfg.Provider, req.ChallengeType)
	certBundle, certRevision, err := buildBundleAndRevision(resource, revisionNumber)
	if err != nil {
		utils.CertLogf("acme: build bundle failed hostname=%s revision=%d err=%v", domain.Hostname, revisionNumber, err)
		return nil, err
	}
	utils.CertLogf("acme: bundle built hostname=%s revision=%d not_before=%s not_after=%s", domain.Hostname, revisionNumber, certRevision.NotBefore.UTC().Format(time.RFC3339), certRevision.NotAfter.UTC().Format(time.RFC3339))
	cert := &metadata.CertificateRevision{
		ID:             "certrev-" + s.idSuffix(),
		Hostname:       domain.Hostname,
		Revision:       revisionNumber,
		Provider:       issuerCfg.Provider,
		ChallengeType:  req.ChallengeType,
		ArtifactBucket: s.cfg.DefaultArtifactBucket,
		ArtifactPrefix: certificateArtifactPrefix(s.cfg.ArtifactPrefix, domain.Hostname, revisionNumber),
		NotBefore:      certRevision.NotBefore,
		NotAfter:       certRevision.NotAfter,
		SHA256Crt:      certRevision.SHA256Crt,
		SHA256Key:      certRevision.SHA256Key,
	}
	if err := s.repo.CertificateRevisions().UpsertResource(ctx, cert); err != nil {
		utils.CertLogf("acme: persist final cert failed hostname=%s cert_id=%s revision=%d err=%v", domain.Hostname, cert.ID, cert.Revision, err)
		return nil, err
	}
	utils.CertLogf("acme: final cert persisted hostname=%s cert_id=%s revision=%d", domain.Hostname, cert.ID, cert.Revision)

	if s.bundles != nil {
		if err := s.bundles.PutCertificateBundle(ctx, certBundle); err != nil {
			utils.CertLogf("acme: store bundle failed hostname=%s revision=%d err=%v", domain.Hostname, revisionNumber, err)
			return nil, err
		}
		utils.CertLogf("acme: bundle stored hostname=%s revision=%d", domain.Hostname, revisionNumber)
	}
	if err := s.notifier(ctx, domain.Hostname); err != nil {
		utils.CertLogf("acme: publish final cert change failed hostname=%s cert_revision_id=%s err=%v", domain.Hostname, cert.ID, err)
		return nil, err
	}
	utils.CertLogf("acme: issue completed hostname=%s provider=%s challenge=%s cert_revision_id=%s revision=%d", domain.Hostname, issuerCfg.Provider, req.ChallengeType, cert.ID, cert.Revision)
	return cert, nil
}

func (s *Service) resolveIssuerConfig(req IssueRequest) (IssuerConfig, error) {
	email := strings.TrimSpace(req.Email)
	if email == "" {
		email = strings.TrimSpace(s.cfg.DefaultEmail)
	}
	if email == "" {
		return IssuerConfig{}, fmt.Errorf("acme email is required")
	}
	cfg := IssuerConfig{
		Provider: req.Provider,
		Email:    email,
	}
	switch req.Provider {
	case metadata.ProviderLetsEncrypt:
		cfg.Directory = lego.LEDirectoryProduction
	case metadata.ProviderZeroSSL:
		eabKID := strings.TrimSpace(req.EABKID)
		if eabKID == "" {
			eabKID = strings.TrimSpace(s.cfg.DefaultEABKID)
		}
		eabHMACKey := strings.TrimSpace(req.EABHMACKey)
		if eabHMACKey == "" {
			eabHMACKey = strings.TrimSpace(s.cfg.DefaultEABHMACKey)
		}
		if eabKID == "" || eabHMACKey == "" {
			return IssuerConfig{}, fmt.Errorf("zerossl requires eab kid and hmac key")
		}
		cfg.Directory = zeroSSLDirectoryURL
		cfg.EABKID = eabKID
		cfg.EABHMACKey = eabHMACKey
	default:
		return IssuerConfig{}, fmt.Errorf("unsupported acme provider %q", req.Provider)
	}
	return cfg, nil
}

func (s *Service) nextRevision(ctx context.Context, hostname string) (uint64, error) {
	cert, err := s.repo.GetLatestCertificateRevision(ctx, hostname)
	if err != nil {
		return 1, nil
	}
	if cert == nil {
		return 1, nil
	}
	return cert.Revision + 1, nil
}
