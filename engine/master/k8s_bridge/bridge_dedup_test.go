package k8s_bridge

import (
	"context"
	"testing"

	"github.com/jabberwocky238/luna-edge/repository/functions"
	"github.com/jabberwocky238/luna-edge/repository/metadata"
	"gorm.io/driver/sqlite"
	"gorm.io/gorm"
)

func TestSyncDomainSetOnlyReturnsChangedHosts(t *testing.T) {
	repo := testBridgeRepository(t)
	ctx := context.Background()
	item := domainMaterialized{
		domain: metadata.DomainEndpoint{
			ID:          "k8s:domain:nginx-gateway.cluster-1.app238.com",
			Hostname:    "nginx-gateway.cluster-1.app238.com",
			NeedCert:    true,
			BackendType: metadata.BackendTypeL7HTTPS,
		},
		backends: []metadata.ServiceBackendRef{{
			ID:               "k8s:backend:test",
			Type:             metadata.ServiceBackendTypeSVC,
			ServiceNamespace: "default",
			ServiceName:      "nginx",
			Port:             443,
		}},
		routes: []metadata.HTTPRoute{{
			ID:               "k8s:route:test",
			DomainEndpointID: "k8s:domain:nginx-gateway.cluster-1.app238.com",
			Path:             "/",
			Priority:         1,
			BackendRefID:     "k8s:backend:test",
		}},
	}

	changedAffected, changedRemoved, err := syncDomainSet(ctx, repo, map[string]domainMaterialized{
		item.domain.Hostname: item,
	}, []string{item.domain.Hostname}, nil)
	if err != nil {
		t.Fatalf("syncDomainSet initial: %v", err)
	}
	if len(changedAffected) != 1 || changedAffected[0] != item.domain.Hostname || len(changedRemoved) != 0 {
		t.Fatalf("unexpected initial changes affected=%v removed=%v", changedAffected, changedRemoved)
	}

	changedAffected, changedRemoved, err = syncDomainSet(ctx, repo, map[string]domainMaterialized{
		item.domain.Hostname: item,
	}, []string{item.domain.Hostname}, nil)
	if err != nil {
		t.Fatalf("syncDomainSet repeat: %v", err)
	}
	if len(changedAffected) != 0 || len(changedRemoved) != 0 {
		t.Fatalf("expected no changes on identical resync, affected=%v removed=%v", changedAffected, changedRemoved)
	}
}

func TestSyncDomainSetMarksCertificateDesiredForHTTPSDomain(t *testing.T) {
	repo := testBridgeRepository(t)
	ctx := context.Background()
	var notified []string
	repo.SetCertificateDesiredNotifier(func(_ context.Context, hostname string) {
		notified = append(notified, hostname)
	})

	host := "nginx-gateway.cluster-1.app238.com"
	_, _, err := syncDomainSet(ctx, repo, map[string]domainMaterialized{
		host: {
			domain: metadata.DomainEndpoint{
				ID:          "k8s:domain:" + host,
				Hostname:    host,
				NeedCert:    true,
				BackendType: metadata.BackendTypeL7HTTPS,
			},
			backends: []metadata.ServiceBackendRef{{
				ID:               "k8s:backend:test",
				Type:             metadata.ServiceBackendTypeSVC,
				ServiceNamespace: "default",
				ServiceName:      "nginx",
				Port:             443,
			}},
			routes: []metadata.HTTPRoute{{
				ID:               "k8s:route:test",
				DomainEndpointID: "k8s:domain:" + host,
				Path:             "/",
				Priority:         1,
				BackendRefID:     "k8s:backend:test",
			}},
		},
	}, []string{host}, nil)
	if err != nil {
		t.Fatalf("syncDomainSet: %v", err)
	}
	if len(notified) != 1 || notified[0] != host {
		t.Fatalf("unexpected certificate desired notifications: %v", notified)
	}
}

func testBridgeRepository(t *testing.T) functions.Repository {
	t.Helper()
	db, err := gorm.Open(sqlite.Open("file::memory:?cache=shared"), &gorm.Config{})
	if err != nil {
		t.Fatalf("open sqlite: %v", err)
	}
	if err := metadata.AutoMigrate(db); err != nil {
		t.Fatalf("auto migrate: %v", err)
	}
	return functions.NewGormRepository(db)
}
