package engine_test

import (
	"context"
	"net"
	"path/filepath"
	"sync"
	"testing"
	"time"

	enginepkg "github.com/jabberwocky238/luna-edge/engine"
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

func TestReplicationRecoversAcrossTwoSlaveFailures(t *testing.T) {
	t.Parallel()

	masterEngine, lis, cleanup := newReplicationMasterForNodes(t, []string{"node-1", "node-2"}, "10.0.0.1", 8080, 7)
	defer cleanup()

	slave1Store := newReplicationLocalStore(t, "slave1")
	defer func() { _ = slave1Store.Close() }()
	slave2Store := newReplicationLocalStore(t, "slave2")
	defer func() { _ = slave2Store.Close() }()

	slave1 := newReplicationSlave(t, "node-1", lis.Addr().String(), slave1Store)
	slave2 := newReplicationSlave(t, "node-2", lis.Addr().String(), slave2Store)

	run1 := startReplicationSlave(t, slave1)
	run2 := startReplicationSlave(t, slave2)
	defer run1.stop()
	defer run2.stop()

	waitForBindingAddress(t, slave1Store, "10.0.0.1", 8080)
	waitForBindingAddress(t, slave2Store, "10.0.0.1", 8080)

	updateMasterBindingForNodes(t, masterEngine.Repo, []string{"node-1", "node-2"}, "10.0.0.2", 8081, 8)
	mustPublishNode(t, masterEngine, "node-1")
	mustPublishNode(t, masterEngine, "node-2")

	waitForBindingAddress(t, slave1Store, "10.0.0.2", 8081)
	waitForBindingAddress(t, slave2Store, "10.0.0.2", 8081)

	run2.stop()

	updateMasterBindingForNodes(t, masterEngine.Repo, []string{"node-1", "node-2"}, "10.0.0.3", 8082, 9)
	mustPublishNode(t, masterEngine, "node-1")
	mustPublishNode(t, masterEngine, "node-2")

	waitForBindingAddress(t, slave1Store, "10.0.0.3", 8082)

	run1.stop()

	updateMasterBindingForNodes(t, masterEngine.Repo, []string{"node-1", "node-2"}, "10.0.0.4", 8083, 10)
	mustPublishNode(t, masterEngine, "node-1")
	mustPublishNode(t, masterEngine, "node-2")

	run1 = startReplicationSlave(t, newReplicationSlave(t, "node-1", lis.Addr().String(), slave1Store))
	run2 = startReplicationSlave(t, newReplicationSlave(t, "node-2", lis.Addr().String(), slave2Store))

	waitForBindingAddress(t, slave1Store, "10.0.0.4", 8083)
	waitForBindingAddress(t, slave2Store, "10.0.0.4", 8083)
	waitForCondition(t, func() bool {
		v1, err1 := slave1Store.GetVersions(context.Background(), "node-1")
		v2, err2 := slave2Store.GetVersions(context.Background(), "node-2")
		return err1 == nil && err2 == nil && v1.DesiredRouteVersion == 10 && v2.DesiredRouteVersion == 10
	})
}

type replicationSlaveRun struct {
	slave  *slavepkg.Engine
	cancel func()
	errCh  chan error
	once   sync.Once
}

func (r *replicationSlaveRun) stop() {
	r.once.Do(func() {
		if r.cancel == nil {
			return
		}
		r.cancel()
		select {
		case err := <-r.errCh:
			if err != nil && err != context.Canceled && status.Code(err) != codes.Canceled {
				panic(err)
			}
		case <-time.After(2 * time.Second):
			panic("timed out waiting for slave to stop")
		}
		if r.slave != nil {
			_ = r.slave.Stop(context.Background())
		}
	})
}

func newReplicationMasterForNodes(t *testing.T, nodeIDs []string, address string, port uint32, version uint64) (*masterpkg.Engine, net.Listener, func()) {
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
	seedMasterProjectionForNodes(t, masterRepo, nodeIDs, address, port, version)

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

func startReplicationSlave(t *testing.T, slave *slavepkg.Engine) *replicationSlaveRun {
	t.Helper()
	ctx, cancel := context.WithCancel(context.Background())
	errCh := make(chan error, 1)
	go func() { errCh <- slave.Start(ctx) }()
	return &replicationSlaveRun{slave: slave, cancel: cancel, errCh: errCh}
}

func seedMasterProjectionForNodes(t *testing.T, repo repository.Repository, nodeIDs []string, address string, port uint32, version uint64) {
	t.Helper()
	ctx := context.Background()
	mustUpsert(t, repo.DomainEndpoints().UpsertResource(ctx, &metadata.DomainEndpoint{
		ID:           "domain-1",
		ZoneID:       "zone-1",
		Hostname:     "app.example.com",
		Generation:   1,
		StateVersion: version,
	}))
	mustUpsert(t, repo.ServiceBindings().UpsertResource(ctx, &metadata.ServiceBinding{
		ID:           "binding-1",
		DomainID:     "domain-1",
		Hostname:     "app.example.com",
		ServiceID:    "svc-1",
		Namespace:    "default",
		Name:         "svc-app",
		Address:      address,
		Port:         port,
		Protocol:     "http",
		RouteVersion: version,
		BackendJSON:  `{"kind":"service"}`,
	}))
	mustUpsert(t, repo.RouteProjections().UpsertResource(ctx, &metadata.RouteProjection{
		DomainID:     "domain-1",
		Hostname:     "app.example.com",
		RouteVersion: version,
		Protocol:     "http",
		RouteJSON:    `{"path":"/"}`,
		BindingID:    "binding-1",
	}))
	for _, nodeID := range nodeIDs {
		mustUpsert(t, repo.Attachments().UpsertResource(ctx, &metadata.Attachment{
			ID:                  "attach-" + nodeID,
			DomainID:            "domain-1",
			NodeID:              nodeID,
			Listener:            "edge-http",
			DesiredRouteVersion: version,
			DesiredDNSVersion:   version,
			State:               metadata.AttachmentStateReady,
		}))
	}
}

func updateMasterBindingForNodes(t *testing.T, repo repository.Repository, nodeIDs []string, address string, port uint32, version uint64) {
	t.Helper()
	ctx := context.Background()
	mustUpsert(t, repo.DomainEndpoints().UpsertResource(ctx, &metadata.DomainEndpoint{
		ID:           "domain-1",
		ZoneID:       "zone-1",
		Hostname:     "app.example.com",
		Generation:   version,
		StateVersion: version,
	}))
	mustUpsert(t, repo.ServiceBindings().UpsertResource(ctx, &metadata.ServiceBinding{
		ID:           "binding-1",
		DomainID:     "domain-1",
		Hostname:     "app.example.com",
		ServiceID:    "svc-1",
		Namespace:    "default",
		Name:         "svc-app",
		Address:      address,
		Port:         port,
		Protocol:     "http",
		RouteVersion: version,
		BackendJSON:  `{"kind":"service"}`,
	}))
	mustUpsert(t, repo.RouteProjections().UpsertResource(ctx, &metadata.RouteProjection{
		DomainID:     "domain-1",
		Hostname:     "app.example.com",
		RouteVersion: version,
		Protocol:     "http",
		RouteJSON:    `{"path":"/"}`,
		BindingID:    "binding-1",
	}))
	for _, nodeID := range nodeIDs {
		mustUpsert(t, repo.Attachments().UpsertResource(ctx, &metadata.Attachment{
			ID:                  "attach-" + nodeID,
			DomainID:            "domain-1",
			NodeID:              nodeID,
			Listener:            "edge-http",
			DesiredRouteVersion: version,
			DesiredDNSVersion:   version,
			State:               metadata.AttachmentStateReady,
		}))
	}
}

func mustPublishNode(t *testing.T, master *masterpkg.Engine, nodeID string) {
	t.Helper()
	if err := master.PublishNode(context.Background(), nodeID); err != nil {
		t.Fatalf("publish node %s: %v", nodeID, err)
	}
}
