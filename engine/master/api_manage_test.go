package master

import (
	"context"
	"testing"

	"github.com/jabberwocky238/luna-edge/lnctl"
	"github.com/jabberwocky238/luna-edge/repository/functions"
	"github.com/jabberwocky238/luna-edge/repository/metadata"
	"gorm.io/driver/sqlite"
	"gorm.io/gorm"
)

func TestApplyPlanCommitsResourcesAndNotifiesCerts(t *testing.T) {
	repo := testManageRepository(t)
	notifier := &stubCertNotifier{notifyCh: make(chan string, 1)}
	api := &manageAPI{
		engine: &Engine{
			NODE_ID: "master-test",
			Repo:    repo,
			Hub:     NewHub(),
			Certs:   &CertReconciler{notifyCh: notifier.notifyCh, doneCh: make(chan struct{})},
		},
	}

	plan := &lnctl.Plan{
		Hostname: "nginx-lnctl.cluster-1.app238.com",
		DomainEndpoints: []lnctl.DomainEndpointChange{{
			Action: lnctl.PlanActionCreate,
			Desired: &metadata.DomainEndpoint{
				Hostname:    "nginx-lnctl.cluster-1.app238.com",
				NeedCert:    true,
				BackendType: metadata.BackendTypeL7HTTPBoth,
			},
		}},
		ServiceBackendRefs: []lnctl.ServiceBackendRefChange{{
			Action: lnctl.PlanActionCreate,
			Desired: &metadata.ServiceBackendRef{
				ID:               "backend:nginx-lnctl.cluster-1.app238.com:root",
				Type:             metadata.ServiceBackendTypeSVC,
				ServiceNamespace: "luna-edge",
				ServiceName:      "nginx-gateway",
				Port:             80,
			},
		}},
		HTTPRoutes: []lnctl.HTTPRouteChange{{
			Action: lnctl.PlanActionCreate,
			Desired: &metadata.HTTPRoute{
				ID:           "route:nginx-lnctl.cluster-1.app238.com:root",
				Hostname:     "nginx-lnctl.cluster-1.app238.com",
				Path:         "/",
				Priority:     1,
				BackendRefID: "backend:nginx-lnctl.cluster-1.app238.com:root",
			},
		}},
	}

	if err := api.applyPlan(context.Background(), plan); err != nil {
		t.Fatalf("applyPlan: %v", err)
	}

	entry, err := repo.GetDomainEntryProjectionByDomain(context.Background(), plan.Hostname)
	if err != nil {
		t.Fatalf("GetDomainEntryProjectionByDomain: %v", err)
	}
	if entry.Hostname != plan.Hostname || !entry.NeedCert {
		t.Fatalf("unexpected projection: %+v", entry)
	}
	if len(entry.HTTPRoutes) != 1 || entry.HTTPRoutes[0].BackendRef == nil {
		t.Fatalf("unexpected routes: %+v", entry.HTTPRoutes)
	}
	if entry.HTTPRoutes[0].BackendRef.ServiceName != "nginx-gateway" {
		t.Fatalf("unexpected backend: %+v", entry.HTTPRoutes[0].BackendRef)
	}

	select {
	case hostname := <-notifier.notifyCh:
		if hostname != plan.Hostname {
			t.Fatalf("unexpected cert notification hostname: %s", hostname)
		}
	default:
		t.Fatal("expected cert notifier to be triggered")
	}

	records, err := repo.ListSnapshotRecordsAfter(context.Background(), 0)
	if err != nil {
		t.Fatalf("ListSnapshotRecordsAfter: %v", err)
	}
	if len(records) != 1 {
		t.Fatalf("expected 1 snapshot record, got %d", len(records))
	}
	if records[0].SyncType != metadata.SnapshotSyncTypeDomainEntryProjection || records[0].SyncID != plan.Hostname {
		t.Fatalf("unexpected snapshot record: %+v", records[0])
	}
}

type stubCertNotifier struct {
	notifyCh chan string
}

func testManageRepository(t *testing.T) functions.Repository {
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
