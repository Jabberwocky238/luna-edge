package master

import (
	"context"
	"fmt"
	"net"
	"net/http"
	"time"

	enginepkg "github.com/jabberwocky238/luna-edge/engine"
	"github.com/jabberwocky238/luna-edge/engine/master/acme"
	masterk8s "github.com/jabberwocky238/luna-edge/engine/master/k8s_bridge"
	"github.com/jabberwocky238/luna-edge/engine/master/manage"
	"github.com/jabberwocky238/luna-edge/replication/replpb"
	"github.com/jabberwocky238/luna-edge/repository"
	"github.com/jabberwocky238/luna-edge/repository/connection"
	"github.com/jabberwocky238/luna-edge/repository/metadata"
	"google.golang.org/grpc"
	"slices"
)

type Config struct {
	StorageDriver         connection.Driver
	SQLitePath            string
	PostgresDSN           string
	AutoMigrate           bool
	ACME                  acme.Config
	S3                    S3Config
	K8sBridgeEnabled      bool
	K8sNamespace          string
	K8sIngressClass       string
	ReplicationListenAddr string
	ManageListenAddr      string
	ShutdownTimeout       time.Duration
}

type Engine struct {
	replpb.UnimplementedReplicationServiceServer

	Config    Config
	Factory   repository.Factory
	Repo      repository.Repository
	Hub       *Hub
	Bundles   CertificateBundleProvider
	Manage    *manage.API
	ACME      *acme.Service
	Certs     *CertReconciler
	K8sBridge *masterk8s.Bridge

	grpcServer   *grpc.Server
	grpcListener net.Listener
	httpServer   *http.Server
	httpListener net.Listener
}

type CertificateBundleProvider interface {
	FetchCertificateBundle(ctx context.Context, hostname string, revision uint64) (*enginepkg.CertificateBundle, error)
	PutCertificateBundle(ctx context.Context, hostname string, revision uint64, bundle *enginepkg.CertificateBundle) error
}

func New(cfg Config) (*Engine, error) {
	if cfg.ShutdownTimeout <= 0 {
		cfg.ShutdownTimeout = 5 * time.Second
	}
	if cfg.StorageDriver == "" {
		cfg.StorageDriver = connection.DriverPostgres
	}
	factory, err := repository.NewFactory(connection.Config{
		Driver:      cfg.StorageDriver,
		DSN:         cfg.PostgresDSN,
		Path:        cfg.SQLitePath,
		AutoMigrate: cfg.AutoMigrate,
	})
	if err != nil {
		return nil, err
	}
	repo := factory.Repository()
	engine := &Engine{
		Config:  cfg,
		Factory: factory,
		Hub:     NewHub(),
	}
	wrapper := manage.NewWrapper(repo, engine, engine)
	engine.Repo = wrapper
	engine.Manage = manage.NewAPI(wrapper)
	if cfg.S3.Enabled() {
		bundles, err := NewS3CertificateBundleProvider(wrapper, cfg.S3)
		if err != nil {
			_ = factory.Close()
			return nil, err
		}
		engine.Bundles = bundles
	}
	engine.ACME = acme.NewService(cfg.ACME, wrapper, engine, engine.Bundles, acme.LegoIssuerFactory{}, engine.Manage)
	engine.Certs = NewCertReconciler(wrapper, engine.ACME, defaultCertReconcileInterval, defaultCertRenewBefore)
	if cfg.K8sBridgeEnabled {
		bridge, err := masterk8s.New(masterk8s.Config{
			Namespace:    cfg.K8sNamespace,
			IngressClass: cfg.K8sIngressClass,
			Enabled:      true,
		}, wrapper)
		if err != nil {
			_ = factory.Close()
			return nil, err
		}
		engine.K8sBridge = bridge
	}
	return engine, nil
}

func (e *Engine) Start() error {
	if e.K8sBridge != nil {
		if err := e.K8sBridge.LoadInitial(context.Background()); err != nil {
			return err
		}
		e.K8sBridge.Listen()
	}
	if e.Certs != nil {
		e.Certs.Start()
	}
	if e.Config.ReplicationListenAddr != "" {
		lis, err := net.Listen("tcp", e.Config.ReplicationListenAddr)
		if err != nil {
			if e.Certs != nil {
				e.Certs.Stop()
			}
			return err
		}
		e.grpcListener = lis
		e.grpcServer = grpc.NewServer()
		replpb.RegisterReplicationServiceServer(e.grpcServer, e)
		go func() { _ = e.grpcServer.Serve(lis) }()
	}
	if e.Config.ManageListenAddr != "" {
		lis, err := net.Listen("tcp", e.Config.ManageListenAddr)
		if err != nil {
			if e.Certs != nil {
				e.Certs.Stop()
			}
			if e.grpcServer != nil {
				e.grpcServer.GracefulStop()
			}
			if e.grpcListener != nil {
				_ = e.grpcListener.Close()
			}
			return err
		}
		e.httpListener = lis
		e.httpServer = &http.Server{Addr: e.Config.ManageListenAddr, Handler: e.Manage.Handler()}
		go func() { _ = e.httpServer.Serve(lis) }()
	}
	return nil
}

func (e *Engine) PublishSnapshot(_ context.Context, snapshot *enginepkg.Snapshot) error {
	if e == nil || e.Hub == nil || snapshot == nil {
		return nil
	}
	for i := range snapshot.DNSRecords {
		recordID, err := e.appendSnapshotRecord(context.Background(), metadata.SnapshotSyncTypeDNSRecord, snapshot.DNSRecords[i].ID, metadata.SnapshotActionUpsert)
		if err != nil {
			return err
		}
		rec := snapshot.DNSRecords[i]
		e.Hub.PublishAll(&enginepkg.ChangeNotification{NodeID: snapshot.NodeID, CreatedAt: time.Now().UTC(), SnapshotRecordID: recordID, DNSRecord: &rec})
	}
	for i := range snapshot.DomainEntries {
		recordID, err := e.appendSnapshotRecord(context.Background(), metadata.SnapshotSyncTypeDomainEntryProjection, snapshot.DomainEntries[i].ID, metadata.SnapshotActionUpsert)
		if err != nil {
			return err
		}
		entry := snapshot.DomainEntries[i]
		e.Hub.PublishAll(&enginepkg.ChangeNotification{NodeID: snapshot.NodeID, CreatedAt: time.Now().UTC(), SnapshotRecordID: recordID, DomainEntry: &entry})
	}
	return nil
}

func (e *Engine) PublishNode(ctx context.Context, nodeID string) error {
	if e == nil || e.Hub == nil {
		return nil
	}
	snapshot, err := e.BuildSnapshot(ctx, nodeID)
	if err != nil {
		return err
	}
	return e.PublishSnapshot(ctx, snapshot)
}

func (e *Engine) BuildSnapshot(ctx context.Context, nodeID string) (*enginepkg.Snapshot, error) {
	snapshot := &enginepkg.Snapshot{
		NodeID:    nodeID,
		CreatedAt: time.Now().UTC(),
	}
	if e == nil || e.Repo == nil {
		return snapshot, nil
	}
	if err := e.Repo.DNSRecords().ListResource(ctx, &snapshot.DNSRecords, "fqdn asc, record_type asc, id asc"); err != nil {
		return nil, err
	}
	var domains []metadata.DomainEndpoint
	if err := e.Repo.DomainEndpoints().ListResource(ctx, &domains, "hostname asc"); err != nil {
		return nil, err
	}
	for i := range domains {
		entry, err := e.Repo.GetDomainEntryProjectionByDomain(ctx, domains[i].Hostname)
		if err == nil && entry != nil {
			snapshot.DomainEntries = append(snapshot.DomainEntries, *entry)
		}
	}
	return snapshot, nil
}

func (e *Engine) appendSnapshotRecord(ctx context.Context, syncType metadata.SnapshotSyncType, syncID string, action metadata.SnapshotAction) (uint64, error) {
	record := &metadata.SnapshotRecord{SyncType: syncType, SyncID: syncID, Action: action}
	if err := e.Repo.AppendSnapshotRecord(ctx, record); err != nil {
		return 0, err
	}
	return record.ID, nil
}

func (e *Engine) GetSnapshot(req *replpb.SnapshotRequest, stream grpc.ServerStreamingServer[replpb.Snapshot]) error {
	records, err := e.Repo.ListSnapshotRecordsAfter(stream.Context(), req.GetSnapshotRecordId())
	if err != nil {
		return err
	}
	chunk := &enginepkg.Snapshot{NodeID: req.GetNodeId(), CreatedAt: time.Now().UTC()}
	count := 0
	lastSeen := req.GetSnapshotRecordId()
	sendChunk := func(last bool) error {
		chunk.Last = last
		chunk.SnapshotRecordID = lastSeen
		if len(chunk.DNSRecords) == 0 && len(chunk.DomainEntries) == 0 && !last {
			return nil
		}
		if err := stream.Send(enginepkg.SnapshotToProto(chunk)); err != nil {
			return err
		}
		chunk = &enginepkg.Snapshot{NodeID: req.GetNodeId(), CreatedAt: time.Now().UTC()}
		count = 0
		return nil
	}
	for i := range records {
		record := records[i]
		lastSeen = record.ID
		switch record.SyncType {
		case metadata.SnapshotSyncTypeDNSRecord:
			item := &metadata.DNSRecord{}
			if err := e.Repo.DNSRecords().GetResourceByField(stream.Context(), item, "id", record.SyncID); err == nil {
				chunk.DNSRecords = append(chunk.DNSRecords, *item)
				count++
			}
		case metadata.SnapshotSyncTypeDomainEntryProjection:
			domain, err := e.Repo.GetDomainEndpointByID(stream.Context(), record.SyncID)
			if err == nil && domain != nil {
				item, projErr := e.Repo.GetDomainEntryProjectionByDomain(stream.Context(), domain.Hostname)
				if projErr == nil && item != nil {
					chunk.DomainEntries = append(chunk.DomainEntries, *item)
					count++
				}
			}
		}
		if count >= 1000 {
			if err := sendChunk(false); err != nil {
				return err
			}
		}
	}
	return sendChunk(true)
}

func (e *Engine) Subscribe(req *replpb.SubscriptionRequest, stream grpc.ServerStreamingServer[replpb.ChangeNotification]) error {
	nodeID := req.GetNodeId()
	subID, ch := e.Hub.Subscribe(nodeID, 128)
	defer e.Hub.Unsubscribe(nodeID, subID)
	for {
		select {
		case <-stream.Context().Done():
			return stream.Context().Err()
		case notice, ok := <-ch:
			if !ok {
				return nil
			}
			if err := stream.Send(enginepkg.ChangeNotificationToProto(notice)); err != nil {
				return err
			}
		}
	}
}

func (e *Engine) FetchCertificateBundle(ctx context.Context, req *replpb.CertificateBundleRequest) (*replpb.CertificateBundleResponse, error) {
	if e == nil || e.Bundles == nil {
		return nil, fmt.Errorf("certificate bundle provider is not configured")
	}
	bundle, err := e.Bundles.FetchCertificateBundle(ctx, req.GetHostname(), req.GetRevision())
	if err != nil {
		return nil, err
	}
	if bundle == nil {
		return nil, fmt.Errorf("certificate bundle not found")
	}
	return &replpb.CertificateBundleResponse{
		Hostname:     bundle.Hostname,
		Revision:     bundle.Revision,
		TlsCrt:       slices.Clone(bundle.TLSCrt),
		TlsKey:       slices.Clone(bundle.TLSKey),
		MetadataJson: slices.Clone(bundle.MetadataJSON),
	}, nil
}

func (e *Engine) Stop(ctx context.Context) error {
	var firstErr error
	if e.Certs != nil {
		e.Certs.Stop()
		e.Certs = nil
	}
	if e.K8sBridge != nil {
		if err := e.K8sBridge.Stop(); err != nil && firstErr == nil {
			firstErr = err
		}
		e.K8sBridge = nil
	}
	if e.httpServer != nil {
		if err := e.httpServer.Shutdown(ctx); err != nil && firstErr == nil {
			firstErr = err
		}
		e.httpServer = nil
	}
	if e.httpListener != nil {
		if err := e.httpListener.Close(); err != nil && firstErr == nil {
			firstErr = err
		}
		e.httpListener = nil
	}
	if e.grpcServer != nil {
		stopped := make(chan struct{})
		go func() {
			e.grpcServer.GracefulStop()
			close(stopped)
		}()
		select {
		case <-ctx.Done():
			e.grpcServer.Stop()
			if firstErr == nil {
				firstErr = ctx.Err()
			}
		case <-stopped:
		}
		e.grpcServer = nil
	}
	if e.grpcListener != nil {
		if err := e.grpcListener.Close(); err != nil && firstErr == nil {
			firstErr = err
		}
		e.grpcListener = nil
	}
	if e.Factory != nil {
		if err := e.Factory.Close(); err != nil && firstErr == nil {
			firstErr = err
		}
		e.Factory = nil
	}
	return firstErr
}

func (e *Engine) Notify(fqdn string) {
	if e == nil || e.Certs == nil {
		return
	}
	e.Certs.Notify(fqdn)
}

func (e *Engine) NotifyCertificateDesired(_ context.Context, fqdn string) error {
	e.Notify(fqdn)
	return nil
}
