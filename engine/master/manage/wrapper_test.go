package manage

import (
	"context"
	"encoding/json"
	"path/filepath"
	"sync"
	"testing"

	"github.com/jabberwocky238/luna-edge/repository"
	"github.com/jabberwocky238/luna-edge/repository/connection"
	"github.com/jabberwocky238/luna-edge/repository/metadata"
)

type fakeNotifier struct {
	mu    sync.Mutex
	fqdns []string
}

func (n *fakeNotifier) NotifyCertificateDesired(_ context.Context, fqdn string) error {
	n.mu.Lock()
	defer n.mu.Unlock()
	n.fqdns = append(n.fqdns, fqdn)
	return nil
}

func (n *fakeNotifier) calls() []string {
	n.mu.Lock()
	defer n.mu.Unlock()
	out := make([]string, len(n.fqdns))
	copy(out, n.fqdns)
	return out
}

func TestWrapperUpsertDomainEndpointNotifiesOnNeedCertEnable(t *testing.T) {
	t.Parallel()

	factory, err := repository.NewFactory(connection.Config{
		Driver:      connection.DriverSQLite,
		Path:        filepath.Join(t.TempDir(), "manage.db"),
		AutoMigrate: true,
	})
	if err != nil {
		t.Fatalf("new factory: %v", err)
	}
	defer func() { _ = factory.Close() }()

	notifier := &fakeNotifier{}
	wrapper := NewWrapper(factory.Repository(), nil, notifier)
	ctx := context.Background()

	first := &metadata.DomainEndpoint{
		ID:          "domain-1",
		Hostname:    "app.example.com",
		NeedCert:    false,
		BackendType: metadata.BackendTypeL7HTTP,
	}
	body, err := json.Marshal(first)
	if err != nil {
		t.Fatalf("marshal first: %v", err)
	}
	if _, err := wrapper.UpsertJSON(ctx, "domain_endpoints", body); err != nil {
		t.Fatalf("upsert first: %v", err)
	}
	if len(notifier.calls()) != 0 {
		t.Fatal("expected no notify for need_cert=false")
	}

	second := &metadata.DomainEndpoint{
		ID:          "domain-1",
		Hostname:    "app.example.com",
		NeedCert:    true,
		BackendType: metadata.BackendTypeL7HTTPS,
	}
	body, err = json.Marshal(second)
	if err != nil {
		t.Fatalf("marshal second: %v", err)
	}
	if _, err := wrapper.UpsertJSON(ctx, "domain_endpoints", body); err != nil {
		t.Fatalf("upsert second: %v", err)
	}

	calls := notifier.calls()
	if len(calls) != 1 || calls[0] != "app.example.com" {
		t.Fatalf("unexpected notify calls: %#v", calls)
	}
}

func TestWrapperUpsertDomainEndpointNotifiesOnCreateWhenNeedCertTrue(t *testing.T) {
	t.Parallel()

	factory, err := repository.NewFactory(connection.Config{
		Driver:      connection.DriverSQLite,
		Path:        filepath.Join(t.TempDir(), "manage.db"),
		AutoMigrate: true,
	})
	if err != nil {
		t.Fatalf("new factory: %v", err)
	}
	defer func() { _ = factory.Close() }()

	notifier := &fakeNotifier{}
	wrapper := NewWrapper(factory.Repository(), nil, notifier)

	body, err := json.Marshal(&metadata.DomainEndpoint{
		ID:          "domain-2",
		Hostname:    "secure.example.com",
		NeedCert:    true,
		BackendType: metadata.BackendTypeL4TLSTermination,
	})
	if err != nil {
		t.Fatalf("marshal: %v", err)
	}
	if _, err := wrapper.UpsertJSON(context.Background(), "domain_endpoints", body); err != nil {
		t.Fatalf("upsert: %v", err)
	}

	calls := notifier.calls()
	if len(calls) != 1 || calls[0] != "secure.example.com" {
		t.Fatalf("unexpected notify calls: %#v", calls)
	}
}

func TestWrapperUpsertDomainEndpointDoesNotNotifyWhenNeedCertUnchanged(t *testing.T) {
	t.Parallel()

	factory, err := repository.NewFactory(connection.Config{
		Driver:      connection.DriverSQLite,
		Path:        filepath.Join(t.TempDir(), "manage.db"),
		AutoMigrate: true,
	})
	if err != nil {
		t.Fatalf("new factory: %v", err)
	}
	defer func() { _ = factory.Close() }()

	notifier := &fakeNotifier{}
	wrapper := NewWrapper(factory.Repository(), nil, notifier)
	ctx := context.Background()

	for _, domain := range []*metadata.DomainEndpoint{
		{
			ID:          "domain-3",
			Hostname:    "same.example.com",
			NeedCert:    true,
			BackendType: metadata.BackendTypeL7HTTPS,
		},
		{
			ID:          "domain-3",
			Hostname:    "same.example.com",
			NeedCert:    true,
			BackendType: metadata.BackendTypeL7HTTPS,
		},
	} {
		body, err := json.Marshal(domain)
		if err != nil {
			t.Fatalf("marshal: %v", err)
		}
		if _, err := wrapper.UpsertJSON(ctx, "domain_endpoints", body); err != nil {
			t.Fatalf("upsert: %v", err)
		}
	}

	calls := notifier.calls()
	if len(calls) != 1 {
		t.Fatalf("expected one notify for initial create only, got %#v", calls)
	}
}
