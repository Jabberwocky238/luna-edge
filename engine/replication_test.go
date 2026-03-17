package engine_test

import (
	"context"
	"net"
	"path/filepath"
	"testing"
	"time"

	masterpkg "github.com/jabberwocky238/luna-edge/engine/master"
	slavepkg "github.com/jabberwocky238/luna-edge/engine/slave"
	"github.com/jabberwocky238/luna-edge/replication/replpb"
	"github.com/jabberwocky238/luna-edge/repository"
	"github.com/jabberwocky238/luna-edge/repository/connection"
	"github.com/jabberwocky238/luna-edge/repository/metadata"
	"google.golang.org/grpc"
	"google.golang.org/grpc/codes"
	"google.golang.org/grpc/status"
)

func TestReplicationMasterToSlavePersistsSnapshotState(t *testing.T) {
	t.Parallel()

	masterEngine, lis, cleanup := newReplicationMaster(t)
	defer cleanup()

	slaveStore := newReplicationLocalStore(t, "slave")
	defer func() { _ = slaveStore.Close() }()
	slaveEngine := newReplicationSlave(t, "node-1", lis.Addr().String(), slaveStore)

	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	errCh := make(chan error, 1)
	go func() { errCh <- slaveEngine.Start(ctx) }()

	waitForCondition(t, func() bool {
		entry, err := slaveStore.GetDomainEntryByHostname(context.Background(), "app.example.com")
		return err == nil && entry != nil && len(entry.HTTPRoutes) == 1 && entry.HTTPRoutes[0].BackendRef != nil && entry.HTTPRoutes[0].BackendRef.ServiceName == "svc-app"
	})
	waitForCondition(t, func() bool {
		records, err := slaveStore.GetDNSRecordsByHostname(context.Background(), "app.example.com")
		return err == nil && len(records) == 1 && records[0].ID == "dns-1"
	})
	waitForCondition(t, func() bool {
		cursor, err := slaveStore.GetSnapshotRecordID(context.Background())
		return err == nil && cursor > 0
	})

	cancel()
	select {
	case err := <-errCh:
		if err != nil && err != context.Canceled && status.Code(err) != codes.Canceled {
			t.Fatalf("unexpected slave start error: %v", err)
		}
	case <-time.After(2 * time.Second):
		t.Fatal("timed out waiting for slave to stop")
	}
	_ = masterEngine
}

func newReplicationLocalStore(t *testing.T, name string) *slavepkg.LocalStore {
	t.Helper()
	store, err := slavepkg.NewLocalStore(filepath.Join(t.TempDir(), name))
	if err != nil {
		t.Fatalf("new local store %s: %v", name, err)
	}
	return store
}

func newReplicationSlave(t *testing.T, nodeID, masterAddress string, store *slavepkg.LocalStore) *slavepkg.Engine {
	t.Helper()
	engine, err := slavepkg.New(slavepkg.Config{
		MasterAddress:     masterAddress,
		SubscribeSnapshot: true,
		RetryMinBackoff:   10 * time.Millisecond,
		RetryMaxBackoff:   20 * time.Millisecond,
	}, filepath.Join(t.TempDir(), nodeID), store, store)
	if err != nil {
		t.Fatalf("new slave %s: %v", nodeID, err)
	}
	return engine
}

func TestReplicationWrapperPublishesFinalStateRefresh(t *testing.T) {
	t.Parallel()

	masterEngine, lis, cleanup := newReplicationMaster(t)
	defer cleanup()

	slaveStore := newReplicationLocalStore(t, "slave")
	defer func() { _ = slaveStore.Close() }()
	slaveEngine := newReplicationSlave(t, "node-1", lis.Addr().String(), slaveStore)

	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	errCh := make(chan error, 1)
	go func() { errCh <- slaveEngine.Start(ctx) }()

	waitForCondition(t, func() bool {
		entry, err := slaveStore.GetDomainEntryByHostname(context.Background(), "app.example.com")
		return err == nil && entry != nil && entry.HTTPRoutes[0].BackendRef.ServicePort == 8080
	})

	updateMasterProjection(t, masterEngine.Repo, "svc-updated", 8089, `["2.2.2.2"]`)
	mustPublishNode(t, masterEngine, "node-1")

	waitForCondition(t, func() bool {
		entry, err := slaveStore.GetDomainEntryByHostname(context.Background(), "app.example.com")
		return err == nil && entry != nil && entry.HTTPRoutes[0].BackendRef.ServiceName == "svc-updated" && entry.HTTPRoutes[0].BackendRef.ServicePort == 8089
	})
	waitForCondition(t, func() bool {
		records, err := slaveStore.GetDNSRecordsByHostname(context.Background(), "app.example.com")
		return err == nil && len(records) == 1 && records[0].ValuesJSON == `["2.2.2.2"]`
	})

	cancel()
	select {
	case err := <-errCh:
		if err != nil && err != context.Canceled && status.Code(err) != codes.Canceled {
			t.Fatalf("unexpected slave start error: %v", err)
		}
	case <-time.After(2 * time.Second):
		t.Fatal("timed out waiting for slave to stop")
	}
}

func TestReplicationReconnectResyncsLatestFinalState(t *testing.T) {
	t.Parallel()

	masterEngine, lis, cleanup := newReplicationMaster(t)
	defer cleanup()

	slaveStore := newReplicationLocalStore(t, "slave")
	defer func() { _ = slaveStore.Close() }()
	slaveEngine := newReplicationSlave(t, "node-1", lis.Addr().String(), slaveStore)

	ctx, cancel := context.WithCancel(context.Background())
	errCh := make(chan error, 1)
	go func() { errCh <- slaveEngine.Start(ctx) }()

	waitForCondition(t, func() bool {
		entry, err := slaveStore.GetDomainEntryByHostname(context.Background(), "app.example.com")
		return err == nil && entry != nil && entry.HTTPRoutes[0].BackendRef.ServicePort == 8080
	})

	cancel()
	select {
	case err := <-errCh:
		if err != nil && err != context.Canceled && status.Code(err) != codes.Canceled {
			t.Fatalf("unexpected slave stop error: %v", err)
		}
	case <-time.After(2 * time.Second):
		t.Fatal("timed out waiting for slave to stop")
	}
	if err := slaveEngine.Stop(context.Background()); err != nil {
		t.Fatalf("stop slave engine: %v", err)
	}

	updateMasterProjection(t, masterEngine.Repo, "svc-restarted", 8087, `["3.3.3.3"]`)
	mustPublishNode(t, masterEngine, "node-1")

	restartedSlave := newReplicationSlave(t, "node-1", lis.Addr().String(), slaveStore)
	restartCtx, restartCancel := context.WithCancel(context.Background())
	defer restartCancel()
	restartErrCh := make(chan error, 1)
	go func() { restartErrCh <- restartedSlave.Start(restartCtx) }()

	waitForCondition(t, func() bool {
		entry, err := slaveStore.GetDomainEntryByHostname(context.Background(), "app.example.com")
		return err == nil && entry != nil && entry.HTTPRoutes[0].BackendRef.ServiceName == "svc-restarted" && entry.HTTPRoutes[0].BackendRef.ServicePort == 8087
	})

	restartCancel()
	select {
	case err := <-restartErrCh:
		if err != nil && err != context.Canceled && status.Code(err) != codes.Canceled {
			t.Fatalf("unexpected restarted slave error: %v", err)
		}
	case <-time.After(2 * time.Second):
		t.Fatal("timed out waiting for restarted slave to stop")
	}
	if err := restartedSlave.Stop(context.Background()); err != nil {
		t.Fatalf("stop restarted slave engine: %v", err)
	}
}

func newReplicationMaster(t *testing.T) (*masterpkg.Engine, net.Listener, func()) {
	t.Helper()

	masterFactory, err := repository.NewFactory(connection.Config{
		Driver:      connection.DriverSQLite,
		Path:        filepath.Join(t.TempDir(), "master.db"),
		AutoMigrate: true,
	})
	if err != nil {
		t.Fatalf("new master factory: %v", err)
	}
	masterRepo := masterFactory.Repository()
	seedMasterProjection(t, masterRepo)

	masterEngine := &masterpkg.Engine{
		Factory: masterFactory,
		Repo:    masterRepo,
		Hub:     masterpkg.NewHub(),
	}

	lis, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatalf("listen: %v", err)
	}
	grpcServer := grpc.NewServer()
	replpb.RegisterReplicationServiceServer(grpcServer, masterEngine)
	go func() { _ = grpcServer.Serve(lis) }()

	return masterEngine, lis, func() {
		grpcServer.Stop()
		_ = lis.Close()
		_ = masterFactory.Close()
	}
}

func seedMasterProjection(t *testing.T, repo repository.Repository) {
	t.Helper()
	ctx := context.Background()
	mustUpsert(t, repo.DomainEndpoints().UpsertResource(ctx, &metadata.DomainEndpoint{
		ID:          "domain-1",
		Hostname:    "app.example.com",
		BackendType: metadata.BackendTypeL7HTTP,
	}))
	mustUpsert(t, repo.ServiceBindingRefs().UpsertResource(ctx, &metadata.ServiceBackendRef{
		ID:               "backend-1",
		ServiceNamespace: "default",
		ServiceName:      "svc-app",
		ServicePort:      8080,
	}))
	mustUpsert(t, repo.HTTPRoutes().UpsertResource(ctx, &metadata.HTTPRoute{
		ID:               "route-1",
		DomainEndpointID: "domain-1",
		Hostname:         "app.example.com",
		Path:             "/",
		Priority:         10,
		BackendRefID:     "backend-1",
	}))
	mustUpsert(t, repo.DNSRecords().UpsertResource(ctx, &metadata.DNSRecord{
		ID:           "dns-1",
		FQDN:         "app.example.com",
		RecordType:   metadata.DNSTypeA,
		RoutingClass: metadata.RoutingClassFirst,
		TTLSeconds:   60,
		ValuesJSON:   `["1.1.1.1"]`,
		Enabled:      true,
	}))
}

func updateMasterProjection(t *testing.T, repo repository.Repository, serviceName string, servicePort uint32, dnsValues string) {
	t.Helper()
	ctx := context.Background()
	mustUpsert(t, repo.ServiceBindingRefs().UpsertResource(ctx, &metadata.ServiceBackendRef{
		ID:               "backend-1",
		ServiceNamespace: "default",
		ServiceName:      serviceName,
		ServicePort:      servicePort,
	}))
	mustUpsert(t, repo.DNSRecords().UpsertResource(ctx, &metadata.DNSRecord{
		ID:           "dns-1",
		FQDN:         "app.example.com",
		RecordType:   metadata.DNSTypeA,
		RoutingClass: metadata.RoutingClassFirst,
		TTLSeconds:   60,
		ValuesJSON:   dnsValues,
		Enabled:      true,
	}))
}

func mustUpsert(t *testing.T, err error) {
	t.Helper()
	if err != nil {
		t.Fatal(err)
	}
}

func waitForCondition(t *testing.T, fn func() bool) {
	t.Helper()
	deadline := time.Now().Add(3 * time.Second)
	for time.Now().Before(deadline) {
		if fn() {
			return
		}
		time.Sleep(20 * time.Millisecond)
	}
	t.Fatal("condition not met before timeout")
}

func mustPublishNode(t *testing.T, master *masterpkg.Engine, nodeID string) {
	t.Helper()
	if err := master.PublishNode(context.Background(), nodeID); err != nil {
		t.Fatalf("publish node %s: %v", nodeID, err)
	}
}
