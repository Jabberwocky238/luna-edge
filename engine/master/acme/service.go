package acme

import (
	"context"
	"fmt"
	"log"
	"strings"
	"time"

	"github.com/go-acme/lego/v4/challenge/dns01"
	"github.com/go-acme/lego/v4/lego"
	"github.com/jabberwocky238/luna-edge/engine"
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
	log.Printf("acme: issue requested domain_id=%s provider=%s challenge=%s", req.DomainID, req.Provider, req.ChallengeType)
	domain, err := s.repo.GetDomainEndpointByID(ctx, req.DomainID)
	if err != nil {
		log.Printf("acme: load domain failed domain_id=%s err=%v", req.DomainID, err)
		return nil, err
	}
	if domain == nil {
		return nil, fmt.Errorf("domain endpoint %q not found", req.DomainID)
	}
	log.Printf("acme: resolved domain domain_id=%s hostname=%s backend_type=%s", domain.ID, domain.Hostname, domain.BackendType)

	issuerCfg, err := s.resolveIssuerConfig(req)
	if err != nil {
		log.Printf("acme: resolve issuer config failed domain_id=%s hostname=%s err=%v", domain.ID, domain.Hostname, err)
		return nil, err
	}
	log.Printf("acme: issuer config hostname=%s provider=%s directory=%s email=%s", domain.Hostname, issuerCfg.Provider, issuerCfg.Directory, issuerCfg.Email)
	revisionNumber, err := s.nextRevision(ctx, domain.ID)
	if err != nil {
		log.Printf("acme: next revision failed domain_id=%s err=%v", domain.ID, err)
		return nil, err
	}
	certID := "certrev-" + s.idSuffix()
	cert := &metadata.CertificateRevision{
		ID:               certID,
		DomainEndpointID: domain.ID,
		Revision:         revisionNumber,
		Provider:         issuerCfg.Provider,
		ChallengeType:    req.ChallengeType,
		ArtifactBucket:   s.cfg.DefaultArtifactBucket,
		ArtifactPrefix:   certificateArtifactPrefix(s.cfg.ArtifactPrefix, domain.Hostname, revisionNumber),
	}
	if err := s.repo.CertificateRevisions().UpsertResource(ctx, cert); err != nil {
		log.Printf("acme: persist placeholder cert failed hostname=%s cert_id=%s revision=%d err=%v", domain.Hostname, certID, revisionNumber, err)
		return nil, err
	}
	log.Printf("acme: placeholder cert persisted hostname=%s cert_id=%s revision=%d", domain.Hostname, certID, revisionNumber)
	if err := s.publishChange(ctx, domain.Hostname); err != nil {
		log.Printf("acme: publish placeholder cert failed hostname=%s cert_id=%s err=%v", domain.Hostname, certID, err)
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
		log.Printf("acme: create issuer failed hostname=%s provider=%s challenge=%s err=%v", domain.Hostname, issuerCfg.Provider, req.ChallengeType, err)
		return nil, err
	}
	log.Printf("acme: issuer created hostname=%s provider=%s challenge=%s order_id=%s", domain.Hostname, issuerCfg.Provider, req.ChallengeType, solver.orderID)

	resource, err := issuer.Obtain(ctx, []string{domain.Hostname})
	if err != nil {
		log.Printf("acme: obtain certificate failed hostname=%s provider=%s challenge=%s err=%v", domain.Hostname, issuerCfg.Provider, req.ChallengeType, err)
		return nil, err
	}
	log.Printf("acme: obtain certificate succeeded hostname=%s provider=%s challenge=%s", domain.Hostname, issuerCfg.Provider, req.ChallengeType)
	bundle, notBefore, notAfter, crtHash, keyHash, err := buildBundle(resource, revisionNumber)
	if err != nil {
		log.Printf("acme: build bundle failed hostname=%s revision=%d err=%v", domain.Hostname, revisionNumber, err)
		return nil, err
	}
	log.Printf("acme: bundle built hostname=%s revision=%d not_before=%s not_after=%s", domain.Hostname, revisionNumber, notBefore.UTC().Format(time.RFC3339), notAfter.UTC().Format(time.RFC3339))

	cert.NotBefore = notBefore
	cert.NotAfter = notAfter
	cert.SHA256Crt = crtHash
	cert.SHA256Key = keyHash
	if err := s.repo.CertificateRevisions().UpsertResource(ctx, cert); err != nil {
		log.Printf("acme: persist final cert failed hostname=%s cert_id=%s revision=%d err=%v", domain.Hostname, cert.ID, cert.Revision, err)
		return nil, err
	}
	log.Printf("acme: final cert persisted hostname=%s cert_id=%s revision=%d", domain.Hostname, cert.ID, cert.Revision)

	if s.bundles != nil {
		if err := s.bundles.PutCertificateBundle(ctx, domain.Hostname, revisionNumber, bundle); err != nil {
			log.Printf("acme: store bundle failed hostname=%s revision=%d err=%v", domain.Hostname, revisionNumber, err)
			return nil, err
		}
		log.Printf("acme: bundle stored hostname=%s revision=%d", domain.Hostname, revisionNumber)
	}
	if err := s.publishChange(ctx, domain.Hostname); err != nil {
		log.Printf("acme: publish final cert change failed hostname=%s cert_revision_id=%s err=%v", domain.Hostname, cert.ID, err)
		return nil, err
	}
	log.Printf("acme: publish final cert change done hostname=%s cert_revision_id=%s", domain.Hostname, cert.ID)
	if err := s.publishChange(ctx, domain.Hostname); err != nil {
		log.Printf("acme: publish post-issue extra change failed hostname=%s cert_revision_id=%s err=%v", domain.Hostname, cert.ID, err)
		return nil, err
	}
	log.Printf("acme: publish post-issue extra change done hostname=%s cert_revision_id=%s", domain.Hostname, cert.ID)
	log.Printf("acme: issue completed hostname=%s provider=%s challenge=%s cert_revision_id=%s revision=%d", domain.Hostname, issuerCfg.Provider, req.ChallengeType, cert.ID, cert.Revision)
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

func (s *Service) publishChange(ctx context.Context, hostname string) error {
	if s == nil || s.publish == nil || s.repo == nil {
		return nil
	}
	entry, err := s.repo.GetDomainEntryProjectionByDomain(ctx, hostname)
	if err != nil {
		log.Printf("acme: publish change load projection failed hostname=%s err=%v", hostname, err)
		return err
	}
	if entry == nil {
		log.Printf("acme: publish change projection missing hostname=%s", hostname)
		return nil
	}
	if entry.Cert != nil {
		log.Printf("acme: publish change projection hostname=%s domain_id=%s cert_id=%s revision=%d", entry.Hostname, entry.ID, entry.Cert.ID, entry.Cert.Revision)
	} else {
		log.Printf("acme: publish change projection hostname=%s domain_id=%s cert=nil", entry.Hostname, entry.ID)
	}
	return s.publish.PublishChangeLog(ctx, &engine.ChangeNotification{
		NodeID:      engine.POD_NAME,
		CreatedAt:   time.Now().UTC(),
		DomainEntry: entry,
	})
}
