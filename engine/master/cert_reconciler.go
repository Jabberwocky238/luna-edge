package master

import (
	"context"
	"fmt"
	"strings"
	"sync"
	"time"

	"github.com/jabberwocky238/luna-edge/engine/master/acme"
	"github.com/jabberwocky238/luna-edge/repository"
	"github.com/jabberwocky238/luna-edge/repository/metadata"
)

const (
	defaultCertReconcileInterval = 5 * time.Minute
	defaultCertRenewBefore       = 30 * 24 * time.Hour
)

type certificateIssuer interface {
	IssueCertificate(ctx context.Context, req acme.IssueRequest) (*metadata.CertificateRevision, error)
}

type CertReconciler struct {
	repo        repository.Repository
	issuer      certificateIssuer
	provider    metadata.ACMEProvider
	interval    time.Duration
	renewBefore time.Duration

	notifyCh chan string
	doneCh   chan struct{}

	mu       sync.Mutex
	inFlight map[string]struct{}
	now      func() time.Time
}

func NewCertReconciler(repo repository.Repository, issuer certificateIssuer, provider metadata.ACMEProvider, interval, renewBefore time.Duration) *CertReconciler {
	if interval <= 0 {
		interval = defaultCertReconcileInterval
	}
	if renewBefore <= 0 {
		renewBefore = defaultCertRenewBefore
	}
	return &CertReconciler{
		repo:        repo,
		issuer:      issuer,
		provider:    provider,
		interval:    interval,
		renewBefore: renewBefore,
		notifyCh:    make(chan string, 128),
		doneCh:      make(chan struct{}),
		inFlight:    map[string]struct{}{},
		now:         func() time.Time { return time.Now().UTC() },
	}
}

func (r *CertReconciler) Start(ctx context.Context) {
	if r == nil {
		return
	}
	if ctx == nil {
		return
	}
	go r.run(ctx)
}

func (r *CertReconciler) Stop() {
	if r == nil {
		return
	}
	<-r.doneCh
}

// wrapper interface
func (r *CertReconciler) NotifyCertificateDesired(_ context.Context, fqdn string) error {
	r.Notify(fqdn)
	return nil
}

func (r *CertReconciler) Notify(fqdn string) {
	if r == nil {
		return
	}
	fqdn = strings.TrimSpace(strings.ToLower(fqdn))
	if fqdn == "" {
		return
	}
	select {
	case r.notifyCh <- fqdn:
	default:
	}
}

func (r *CertReconciler) run(ctx context.Context) {
	defer close(r.doneCh)
	ticker := time.NewTicker(r.interval)
	defer ticker.Stop()

	_ = r.scan(ctx)
	for {
		select {
		case <-ctx.Done():
			return
		case <-ticker.C:
			_ = r.scan(ctx)
		case fqdn := <-r.notifyCh:
			_ = r.reconcileHostname(ctx, fqdn)
		}
	}
}

func (r *CertReconciler) scan(ctx context.Context) error {
	if r == nil || r.repo == nil || r.issuer == nil {
		return nil
	}
	var domains []metadata.DomainEndpoint
	if err := r.repo.DomainEndpoints().ListResource(ctx, &domains, "hostname asc"); err != nil {
		return err
	}
	for i := range domains {
		if err := r.reconcileDomain(ctx, &domains[i]); err != nil {
			continue
		}
	}
	return nil
}

func (r *CertReconciler) reconcileHostname(ctx context.Context, fqdn string) error {
	if r == nil || r.repo == nil {
		return nil
	}
	domain, err := r.repo.GetDomainEndpointByHostname(ctx, fqdn)
	if err != nil {
		return err
	}
	if domain == nil {
		return fmt.Errorf("domain endpoint %q not found", fqdn)
	}
	return r.reconcileDomain(ctx, domain)
}

func (r *CertReconciler) reconcileDomain(ctx context.Context, domain *metadata.DomainEndpoint) error {
	if domain == nil || !domain.NeedCert {
		return nil
	}
	if !r.begin(domain.Hostname) {
		return nil
	}
	defer r.end(domain.Hostname)

	if !r.shouldIssue(ctx, domain) {
		return nil
	}
	_, err := r.issuer.IssueCertificate(ctx, acme.IssueRequest{
		DomainID:      domain.ID,
		ChallengeType: challengeTypeForDomain(domain),
		Provider:      r.provider,
	})
	return err
}

func (r *CertReconciler) shouldIssue(ctx context.Context, domain *metadata.DomainEndpoint) bool {
	cert, err := r.repo.GetActiveCertificateForDomain(ctx, domain)
	if err != nil || cert == nil {
		return true
	}
	if cert.NotAfter.IsZero() {
		return true
	}
	return !cert.NotAfter.After(r.now().Add(r.renewBefore))
}

func (r *CertReconciler) begin(fqdn string) bool {
	r.mu.Lock()
	defer r.mu.Unlock()
	if _, ok := r.inFlight[fqdn]; ok {
		return false
	}
	r.inFlight[fqdn] = struct{}{}
	return true
}

func (r *CertReconciler) end(fqdn string) {
	r.mu.Lock()
	defer r.mu.Unlock()
	delete(r.inFlight, fqdn)
}

func challengeTypeForDomain(domain *metadata.DomainEndpoint) metadata.ChallengeType {
	switch domain.BackendType {
	case metadata.BackendTypeL7HTTP, metadata.BackendTypeL7HTTPS, metadata.BackendTypeL7HTTPBoth:
		return metadata.ChallengeTypeHTTP01
	default:
		return metadata.ChallengeTypeDNS01
	}
}
