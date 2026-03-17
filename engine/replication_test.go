package engine_test

import (
	"context"
	"net"
	"path/filepath"
	"testing"
	"time"

	enginepkg "github.com/jabberwocky238/luna-edge/engine"
	masterpkg "github.com/jabberwocky238/luna-edge/engine/master"
	"github.com/jabberwocky238/luna-edge/engine/master/manage"
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
		binding, err := slaveStore.GetBindingByHostname(context.Background(), "app.example.com")
		return err == nil && binding != nil && binding.Address == "10.0.0.1"
	})
	waitForCondition(t, func() bool {
		route, err := slaveStore.GetRouteByHostname(context.Background(), "app.example.com")
		return err == nil && route != nil && route.BindingID == "binding-1"
	})
	waitForCondition(t, func() bool {
		assignments, err := slaveStore.ListAssignments(context.Background(), "node-1")
		return err == nil && len(assignments) == 1 && assignments[0].Hostname == "app.example.com"
	})
	waitForCondition(t, func() bool {
		versions, err := slaveStore.GetVersions(context.Background(), "node-1")
		return err == nil && versions.DesiredRouteVersion == 7 && versions.DesiredDNSVersion == 7
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
		NodeID:            nodeID,
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

func waitForBindingAddress(t *testing.T, store *slavepkg.LocalStore, address string, port uint32) {
	t.Helper()
	waitForCondition(t, func() bool {
		binding, err := store.GetBindingByHostname(context.Background(), "app.example.com")
		return err == nil && binding != nil && binding.Address == address && binding.Port == port
	})
}

func TestReplicationWrapperPublishesFinalStateRefresh(t *testing.T) {
	t.Parallel()

	masterEngine, lis, cleanup := newReplicationMaster(t)
	defer cleanup()
	wrapper := manage.NewWrapper(masterEngine.Repo, masterEngine.Builder, masterEngine)

	slaveStore := newReplicationLocalStore(t, "slave")
	defer func() { _ = slaveStore.Close() }()
	slaveEngine := newReplicationSlave(t, "node-1", lis.Addr().String(), slaveStore)

	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	errCh := make(chan error, 1)
	go func() { errCh <- slaveEngine.Start(ctx) }()

	waitForBindingAddress(t, slaveStore, "10.0.0.1", 8080)

	body := []byte(`{"id":"binding-1","domain_id":"domain-1","hostname":"app.example.com","service_id":"svc-1","namespace":"default","name":"svc-app","address":"10.0.0.9","port":8089,"protocol":"http","route_version":9,"backend_json":"{\"kind\":\"service\"}"}`)
	if _, err := wrapper.UpsertJSON(context.Background(), "service_bindings", body); err != nil {
		t.Fatalf("wrapper upsert binding: %v", err)
	}

	waitForBindingAddress(t, slaveStore, "10.0.0.9", 8089)
	waitForCondition(t, func() bool {
		versions, err := slaveStore.GetVersions(context.Background(), "node-1")
		return err == nil && versions.DesiredRouteVersion == 7
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
	wrapper := manage.NewWrapper(masterEngine.Repo, masterEngine.Builder, masterEngine)

	slaveStore := newReplicationLocalStore(t, "slave")
	defer func() { _ = slaveStore.Close() }()
	slaveEngine := newReplicationSlave(t, "node-1", lis.Addr().String(), slaveStore)

	ctx, cancel := context.WithCancel(context.Background())
	errCh := make(chan error, 1)
	go func() { errCh <- slaveEngine.Start(ctx) }()

	waitForBindingAddress(t, slaveStore, "10.0.0.1", 8080)

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

	body := []byte(`{"id":"binding-1","domain_id":"domain-1","hostname":"app.example.com","service_id":"svc-1","namespace":"default","name":"svc-app","address":"10.0.0.7","port":8087,"protocol":"http","route_version":11,"backend_json":"{\"kind\":\"service\"}"}`)
	if _, err := wrapper.UpsertJSON(context.Background(), "service_bindings", body); err != nil {
		t.Fatalf("wrapper upsert binding while offline: %v", err)
	}

	restartedSlave := newReplicationSlave(t, "node-1", lis.Addr().String(), slaveStore)
	restartCtx, restartCancel := context.WithCancel(context.Background())
	defer restartCancel()
	restartErrCh := make(chan error, 1)
	go func() { restartErrCh <- restartedSlave.Start(restartCtx) }()

	waitForBindingAddress(t, slaveStore, "10.0.0.7", 8087)

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

	builder, err := enginepkg.NewRepositoryProjectionBuilder(masterRepo)
	if err != nil {
		t.Fatalf("new builder: %v", err)
	}
	masterEngine := &masterpkg.Engine{
		Factory: masterFactory,
		Repo:    masterRepo,
		Hub:     masterpkg.NewHub(),
		Builder: builder,
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
		ID:           "domain-1",
		ZoneID:       "zone-1",
		Hostname:     "app.example.com",
		Generation:   1,
		StateVersion: 7,
	}))
	mustUpsert(t, repo.ServiceBindings().UpsertResource(ctx, &metadata.ServiceBinding{
		ID:           "binding-1",
		DomainID:     "domain-1",
		Hostname:     "app.example.com",
		ServiceID:    "svc-1",
		Namespace:    "default",
		Name:         "svc-app",
		Address:      "10.0.0.1",
		Port:         8080,
		Protocol:     "http",
		RouteVersion: 1,
		BackendJSON:  `{"kind":"service"}`,
	}))
	mustUpsert(t, repo.Attachments().UpsertResource(ctx, &metadata.Attachment{
		ID:                  "attach-1",
		DomainID:            "domain-1",
		NodeID:              "node-1",
		Listener:            "edge-http",
		DesiredRouteVersion: 7,
		DesiredDNSVersion:   7,
		State:               metadata.AttachmentStateReady,
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
