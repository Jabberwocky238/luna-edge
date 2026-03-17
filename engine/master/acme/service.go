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

func NewService(cfg Config, repo repository.Repository, publish publisher, bundles bundleStore) *Service {
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
		issuers:  LegoIssuerFactory{},
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
	zone := &metadata.Zone{}
	if err := s.repo.Zones().GetResourceByField(ctx, zone, "id", domain.ZoneID); err != nil {
		return nil, err
	}

	issuerCfg, err := s.resolveIssuerConfig(zone, req)
	if err != nil {
		return nil, err
	}
	revisionNumber, err := s.nextRevision(ctx, domain.ID)
	if err != nil {
		return nil, err
	}
	certID := "certrev-" + s.idSuffix()
	orderID := "acmeorder-" + s.idSuffix()
	cert := &metadata.CertificateRevision{
		ID:             certID,
		DomainID:       domain.ID,
		ZoneID:         domain.ZoneID,
		Hostname:       domain.Hostname,
		Revision:       revisionNumber,
		Provider:       issuerCfg.Provider,
		ChallengeType:  req.ChallengeType,
		Status:         metadata.CertificateRevisionStatusPending,
		ArtifactBucket: s.cfg.DefaultArtifactBucket,
		ArtifactPrefix: certificateArtifactPrefix(s.cfg.ArtifactPrefix, domain.Hostname, revisionNumber),
	}
	if err := s.repo.CertificateRevisions().UpsertResource(ctx, cert); err != nil {
		return nil, err
	}
	order := &metadata.ACMEOrder{
		ID:                    orderID,
		DomainID:              domain.ID,
		CertificateRevisionID: certID,
		Provider:              issuerCfg.Provider,
		AccountRef:            issuerCfg.Email,
		Status:                metadata.ACMEOrderStatusPending,
		StartedAt:             s.now(),
	}
	if err := s.repo.ACMEOrders().UpsertResource(ctx, order); err != nil {
		return nil, err
	}
	if err := s.markIssuing(ctx, domain, revisionNumber); err != nil {
		return nil, err
	}

	solver := &masterChallengeProvider{
		service:                s,
		domain:                 domain,
		orderID:                orderID,
		challengeType:          req.ChallengeType,
		timeout:                s.cfg.DNS01Timeout,
		interval:               s.cfg.DNS01Interval,
	}
	issuer, err := s.issuers.New(issuerCfg, req.ChallengeType, solver)
	if err != nil {
		_ = s.failOrder(ctx, order, cert, err)
		return nil, err
	}

	resource, err := issuer.Obtain(ctx, []string{domain.Hostname})
	if err != nil {
		_ = s.failOrder(ctx, order, cert, err)
		return nil, err
	}
	bundle, notBefore, notAfter, crtHash, keyHash, err := buildBundle(resource, revisionNumber)
	if err != nil {
		_ = s.failOrder(ctx, order, cert, err)
		return nil, err
	}

	cert.Status = metadata.CertificateRevisionStatusActive
	cert.NotBefore = notBefore
	cert.NotAfter = notAfter
	cert.SHA256Crt = crtHash
	cert.SHA256Key = keyHash
	cert.Version = fmt.Sprintf("%d", revisionNumber)
	if err := s.repo.CertificateRevisions().UpsertResource(ctx, cert); err != nil {
		return nil, err
	}
	if s.bundles != nil {
		if err := s.bundles.PutCertificateBundle(ctx, domain.Hostname, revisionNumber, bundle); err != nil {
			_ = s.failOrder(ctx, order, cert, err)
			return nil, err
		}
	}

	now := s.now()
	order.Status = metadata.ACMEOrderStatusValid
	order.CompletedAt = now
	if err := s.repo.ACMEOrders().UpsertResource(ctx, order); err != nil {
		return nil, err
	}
	status := &metadata.DomainEndpointStatus{
		DomainEndpointID:    domain.ID,
		ObservedGeneration:  domain.Generation,
		ChallengeReady:      true,
		CertificateReady:    true,
		CertificateRevision: revisionNumber,
		RouteReady:          true,
		AttachmentReady:     true,
		Ready:               true,
		Phase:               metadata.DomainPhaseReady,
		UpdatedAt:           now,
	}
	if err := s.repo.DomainEndpointStatuses().UpsertResource(ctx, status); err != nil {
		return nil, err
	}
	if err := s.publishDomain(ctx, domain.ID); err != nil {
		return nil, err
	}
	return cert, nil
}

func (s *Service) resolveIssuerConfig(zone *metadata.Zone, req IssueRequest) (IssuerConfig, error) {
	provider := strings.ToLower(strings.TrimSpace(req.Provider))
	if provider == "" && zone != nil {
		provider = strings.ToLower(strings.TrimSpace(zone.DefaultACMEProvider))
	}
	if provider == "" {
		provider = ProviderLetsEncrypt
	}
	email := strings.TrimSpace(req.Email)
	if email == "" {
		email = strings.TrimSpace(s.cfg.DefaultEmail)
	}
	if email == "" {
		return IssuerConfig{}, fmt.Errorf("acme email is required")
	}
	cfg := IssuerConfig{
		Provider: provider,
		Email:    email,
	}
	switch provider {
	case ProviderLetsEncrypt:
		cfg.Directory = lego.LEDirectoryProduction
	case ProviderZeroSSL:
		if strings.TrimSpace(req.EABKID) == "" || strings.TrimSpace(req.EABHMACKey) == "" {
			return IssuerConfig{}, fmt.Errorf("zerossl requires eab kid and hmac key")
		}
		cfg.Directory = zeroSSLDirectoryURL
		cfg.EABKID = strings.TrimSpace(req.EABKID)
		cfg.EABHMACKey = strings.TrimSpace(req.EABHMACKey)
	default:
		return IssuerConfig{}, fmt.Errorf("unsupported acme provider %q", provider)
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

func (s *Service) publishDomain(ctx context.Context, domainID string) error {
	if s.publish == nil {
		return nil
	}
	attachments, err := s.repo.ListAttachmentsByDomainID(ctx, domainID)
	if err != nil {
		return err
	}
	seen := map[string]struct{}{}
	for i := range attachments {
		if _, ok := seen[attachments[i].NodeID]; ok {
			continue
		}
		if err := s.publish.PublishNode(ctx, attachments[i].NodeID); err != nil {
			return err
		}
		seen[attachments[i].NodeID] = struct{}{}
	}
	return nil
}

func (s *Service) markIssuing(ctx context.Context, domain *metadata.DomainEndpoint, revision uint64) error {
	if err := s.repo.DomainEndpointStatuses().UpsertResource(ctx, &metadata.DomainEndpointStatus{
		DomainEndpointID:    domain.ID,
		ObservedGeneration:  domain.Generation,
		Phase:               metadata.DomainPhaseIssuing,
		CertificateReady:    false,
		ChallengeReady:      false,
		CertificateRevision: revision,
		UpdatedAt:           s.now(),
	}); err != nil {
		return err
	}
	return s.publishDomain(ctx, domain.ID)
}

func (s *Service) failOrder(ctx context.Context, order *metadata.ACMEOrder, cert *metadata.CertificateRevision, issueErr error) error {
	if order != nil {
		order.Status = metadata.ACMEOrderStatusInvalid
		order.ErrorMessage = issueErr.Error()
		order.CompletedAt = s.now()
		_ = s.repo.ACMEOrders().UpsertResource(ctx, order)
	}
	if cert != nil {
		cert.Status = metadata.CertificateRevisionStatusFailed
		_ = s.repo.CertificateRevisions().UpsertResource(ctx, cert)
	}
	if order != nil {
		var revision uint64
		if cert != nil {
			revision = cert.Revision
		}
		_ = s.repo.DomainEndpointStatuses().UpsertResource(ctx, &metadata.DomainEndpointStatus{
			DomainEndpointID:    order.DomainID,
			Phase:               metadata.DomainPhaseError,
			CertificateReady:    false,
			ChallengeReady:      false,
			CertificateRevision: revision,
			Ready:               false,
			LastError:           issueErr.Error(),
			LastErrorAt:         s.now(),
			UpdatedAt:           s.now(),
		})
		_ = s.publishDomain(ctx, order.DomainID)
	}
	return issueErr
}
