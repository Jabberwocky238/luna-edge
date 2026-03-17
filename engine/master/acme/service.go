package acme

import (
	"context"
	"fmt"
	"strings"
	"time"

	"github.com/go-acme/lego/v4/challenge/dns01"
	"github.com/go-acme/lego/v4/lego"
	"github.com/jabberwocky238/luna-edge/repository"
	"github.com/jabberwocky238/luna-edge/repository/metadata"
)

func NewService(cfg Config, repo repository.Repository, publish publisher, bundles bundleStore, issuer IssuerFactory, http01 http01ChallengeStore) *Service {
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
		publish:  publish,
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
	domain, err := s.repo.GetDomainEndpointByID(ctx, req.DomainID)
	if err != nil {
		return nil, err
	}
	if domain == nil {
		return nil, fmt.Errorf("domain endpoint %q not found", req.DomainID)
	}

	issuerCfg, err := s.resolveIssuerConfig(req)
	if err != nil {
		return nil, err
	}
	revisionNumber, err := s.nextRevision(ctx, domain.ID)
	if err != nil {
		return nil, err
	}
	certID := "certrev-" + s.idSuffix()
	cert := &metadata.CertificateRevision{
		ID:               certID,
		DomainEndpointID: domain.ID,
		Hostname:         domain.Hostname,
		Revision:         revisionNumber,
		Provider:         issuerCfg.Provider,
		ChallengeType:    req.ChallengeType,
		ArtifactBucket:   s.cfg.DefaultArtifactBucket,
		ArtifactPrefix:   certificateArtifactPrefix(s.cfg.ArtifactPrefix, domain.Hostname, revisionNumber),
	}
	if err := s.repo.CertificateRevisions().UpsertResource(ctx, cert); err != nil {
		return nil, err
	}
	if err := s.publishChange(ctx); err != nil {
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
		return nil, err
	}

	resource, err := issuer.Obtain(ctx, []string{domain.Hostname})
	if err != nil {
		return nil, err
	}
	bundle, notBefore, notAfter, crtHash, keyHash, err := buildBundle(resource, revisionNumber)
	if err != nil {
		return nil, err
	}

	cert.NotBefore = notBefore
	cert.NotAfter = notAfter
	cert.SHA256Crt = crtHash
	cert.SHA256Key = keyHash
	if err := s.repo.CertificateRevisions().UpsertResource(ctx, cert); err != nil {
		return nil, err
	}
	if s.bundles != nil {
		if err := s.bundles.PutCertificateBundle(ctx, domain.Hostname, revisionNumber, bundle); err != nil {
			return nil, err
		}
	}

	domain.CertID = cert.ID
	if err := s.repo.DomainEndpoints().UpsertResource(ctx, domain); err != nil {
		return nil, err
	}
	if err := s.publishChange(ctx); err != nil {
		return nil, err
	}
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

func (s *Service) nextRevision(ctx context.Context, domainID string) (uint64, error) {
	cert, err := s.repo.GetLatestCertificateRevision(ctx, domainID)
	if err != nil {
		return 1, nil
	}
	if cert == nil {
		return 1, nil
	}
	return cert.Revision + 1, nil
}

func (s *Service) publishChange(ctx context.Context) error {
	if s.publish == nil {
		return nil
	}
	return s.publish.PublishNode(ctx, "")
}
