package engine_test

import (
	"context"
	"net"
	"path/filepath"
	"sync"
	"testing"
	"time"

	masterpkg "github.com/jabberwocky238/luna-edge/engine/master"
	slavepkg "github.com/jabberwocky238/luna-edge/engine/slave"
	"github.com/jabberwocky238/luna-edge/replication/replpb"
	"github.com/jabberwocky238/luna-edge/repository"
	"github.com/jabberwocky238/luna-edge/repository/connection"
	"google.golang.org/grpc"
	"google.golang.org/grpc/codes"
	"google.golang.org/grpc/status"
)

func TestReplicationRecoversAcrossTwoSlaveFailures(t *testing.T) {
	t.Parallel()

	masterEngine, lis, cleanup := newReplicationMasterForRecover(t, "svc-a", 8080)
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

	waitForServiceName(t, slave1Store, "svc-a", 8080)
	waitForServiceName(t, slave2Store, "svc-a", 8080)

	updateMasterProjection(t, masterEngine.Repo, "svc-b", 8081, `["2.2.2.2"]`)
	mustPublishNode(t, masterEngine, "node-1")
	mustPublishNode(t, masterEngine, "node-2")

	waitForServiceName(t, slave1Store, "svc-b", 8081)
	waitForServiceName(t, slave2Store, "svc-b", 8081)

	run2.stop()

	updateMasterProjection(t, masterEngine.Repo, "svc-c", 8082, `["3.3.3.3"]`)
	mustPublishNode(t, masterEngine, "node-1")
	mustPublishNode(t, masterEngine, "node-2")

	waitForServiceName(t, slave1Store, "svc-c", 8082)

	run1.stop()

	updateMasterProjection(t, masterEngine.Repo, "svc-d", 8083, `["4.4.4.4"]`)
	mustPublishNode(t, masterEngine, "node-1")
	mustPublishNode(t, masterEngine, "node-2")

	run1 = startReplicationSlave(t, newReplicationSlave(t, "node-1", lis.Addr().String(), slave1Store))
	run2 = startReplicationSlave(t, newReplicationSlave(t, "node-2", lis.Addr().String(), slave2Store))

	waitForServiceName(t, slave1Store, "svc-d", 8083)
	waitForServiceName(t, slave2Store, "svc-d", 8083)
	waitForCondition(t, func() bool {
		v1, err1 := slave1Store.GetSnapshotRecordID(context.Background())
		v2, err2 := slave2Store.GetSnapshotRecordID(context.Background())
		return err1 == nil && err2 == nil && v1 > 0 && v2 > 0
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

func newReplicationMasterForRecover(t *testing.T, serviceName string, port uint32) (*masterpkg.Engine, net.Listener, func()) {
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
	updateMasterProjection(t, masterRepo, serviceName, port, `["1.1.1.1"]`)

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

func startReplicationSlave(t *testing.T, slave *slavepkg.Engine) *replicationSlaveRun {
	t.Helper()
	ctx, cancel := context.WithCancel(context.Background())
	errCh := make(chan error, 1)
	go func() { errCh <- slave.Start(ctx) }()
	return &replicationSlaveRun{slave: slave, cancel: cancel, errCh: errCh}
}

func waitForServiceName(t *testing.T, store *slavepkg.LocalStore, serviceName string, port uint32) {
	t.Helper()
	waitForCondition(t, func() bool {
		entry, err := store.GetDomainEntryByHostname(context.Background(), "app.example.com")
		return err == nil && entry != nil && len(entry.HTTPRoutes) == 1 && entry.HTTPRoutes[0].BackendRef != nil &&
			entry.HTTPRoutes[0].BackendRef.ServiceName == serviceName && entry.HTTPRoutes[0].BackendRef.ServicePort == port
	})
}
