package master

import (
	"context"
	"fmt"
	"log"
	"net"
	"net/http"
	"time"

	"slices"

	enginepkg "github.com/jabberwocky238/luna-edge/engine"
	"github.com/jabberwocky238/luna-edge/engine/master/acme"
	masterk8s "github.com/jabberwocky238/luna-edge/engine/master/k8s_bridge"
	"github.com/jabberwocky238/luna-edge/engine/master/manage"
	"github.com/jabberwocky238/luna-edge/replication/replpb"
	"github.com/jabberwocky238/luna-edge/repository"
	"github.com/jabberwocky238/luna-edge/repository/connection"
	"github.com/jabberwocky238/luna-edge/repository/metadata"
	"google.golang.org/grpc"
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

	ctx    context.Context
	cancel context.CancelFunc

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
	bundles, err := NewS3CertificateBundleProvider(wrapper, cfg.S3)
	if err != nil {
		_ = factory.Close()
		return nil, err
	}
	engine.Bundles = bundles
	engine.ACME = acme.NewService(cfg.ACME, wrapper, engine, engine.Bundles, acme.LegoIssuerFactory{}, engine.Manage)
	engine.Certs = NewCertReconciler(wrapper, engine.ACME, cfg.ACME.DefaultProvider, defaultCertReconcileInterval, defaultCertRenewBefore)
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

func (e *Engine) Start(ctx context.Context, cancel context.CancelFunc) error {
	if ctx == nil {
		return fmt.Errorf("start context is required")
	}
	e.ctx = ctx
	e.cancel = cancel
	defer func() {
		if err := e.shutdown(); err != nil {
			log.Printf("master: shutdown failed err=%v", err)
		}
	}()
	log.Printf("master: start begin replication=%s manage=%s k8s_bridge=%v", e.Config.ReplicationListenAddr, e.Config.ManageListenAddr, e.K8sBridge != nil)
	if e.K8sBridge != nil {
		log.Printf("master: k8s bridge load initial begin")
		if err := e.K8sBridge.LoadInitial(ctx); err != nil {
			log.Printf("master: k8s bridge load initial failed err=%v", err)
			return err
		}
		log.Printf("master: k8s bridge load initial done")
		e.K8sBridge.Listen(ctx)
		log.Printf("master: k8s bridge listeners started")
	}
	if e.Certs != nil {
		e.Certs.Start(ctx)
		log.Printf("master: cert reconciler started")
	}
	if e.Config.ReplicationListenAddr != "" {
		lis, err := net.Listen("tcp", e.Config.ReplicationListenAddr)
		if err != nil {
			return err
		}
		e.grpcListener = lis
		e.grpcServer = grpc.NewServer()
		replpb.RegisterReplicationServiceServer(e.grpcServer, e)
		log.Printf("master: replication listener ready addr=%s", lis.Addr().String())
		go func() { _ = e.grpcServer.Serve(lis) }()
	}
	if e.Config.ManageListenAddr != "" {
		lis, err := net.Listen("tcp", e.Config.ManageListenAddr)
		if err != nil {
			return err
		}
		e.httpListener = lis
		e.httpServer = &http.Server{Addr: e.Config.ManageListenAddr, Handler: e.Manage.Handler()}
		log.Printf("master: manage listener ready addr=%s", lis.Addr().String())
		go func() { _ = e.httpServer.Serve(lis) }()
	}
	log.Printf("master: start done")
	<-ctx.Done()
	log.Printf("master: context done err=%v", ctx.Err())
	return ctx.Err()
}

func (e *Engine) PublishSnapshot(ctx context.Context, snapshot *enginepkg.Snapshot) error {
	if e == nil || e.Hub == nil || snapshot == nil {
		return nil
	}
	log.Printf("replication: publish snapshot begin node_id=%s dns=%d domains=%d", snapshot.NodeID, len(snapshot.DNSRecords), len(snapshot.DomainEntries))
	for i := range snapshot.DNSRecords {
		recordID, err := e.appendSnapshotRecord(ctx, metadata.SnapshotSyncTypeDNSRecord, snapshot.DNSRecords[i].ID, metadata.SnapshotActionUpsert)
		if err != nil {
			log.Printf("replication: append snapshot record failed type=%s sync_id=%s err=%v", metadata.SnapshotSyncTypeDNSRecord, snapshot.DNSRecords[i].ID, err)
			return err
		}
		rec := snapshot.DNSRecords[i]
		log.Printf("replication: publish dns change snapshot_record_id=%d dns_id=%s fqdn=%s type=%s", recordID, rec.ID, rec.FQDN, rec.RecordType)
		e.Hub.PublishAll(&enginepkg.ChangeNotification{NodeID: snapshot.NodeID, CreatedAt: time.Now().UTC(), SnapshotRecordID: recordID, DNSRecord: &rec})
	}
	for i := range snapshot.DomainEntries {
		recordID, err := e.appendSnapshotRecord(ctx, metadata.SnapshotSyncTypeDomainEntryProjection, snapshot.DomainEntries[i].ID, metadata.SnapshotActionUpsert)
		if err != nil {
			log.Printf("replication: append snapshot record failed type=%s sync_id=%s err=%v", metadata.SnapshotSyncTypeDomainEntryProjection, snapshot.DomainEntries[i].ID, err)
			return err
		}
		entry := snapshot.DomainEntries[i]
		log.Printf("replication: publish domain change snapshot_record_id=%d domain_id=%s hostname=%s", recordID, entry.ID, entry.Hostname)
		e.Hub.PublishAll(&enginepkg.ChangeNotification{NodeID: snapshot.NodeID, CreatedAt: time.Now().UTC(), SnapshotRecordID: recordID, DomainEntry: &entry})
	}
	log.Printf("replication: publish snapshot done node_id=%s", snapshot.NodeID)
	return nil
}

func (e *Engine) PublishChangeLog(ctx context.Context, changelog *enginepkg.ChangeNotification) error {
	if e == nil || e.Hub == nil || changelog == nil {
		return nil
	}
	log.Printf("replication: publish changelog begin node_id=%s dns=%v domain=%v", changelog.NodeID, changelog.DNSRecord != nil, changelog.DomainEntry != nil)
	switch {
	case changelog.DNSRecord != nil:
		recordID, err := e.appendSnapshotRecord(ctx, metadata.SnapshotSyncTypeDNSRecord, changelog.DNSRecord.ID, metadata.SnapshotActionUpsert)
		if err != nil {
			return err
		}
		changelog.SnapshotRecordID = recordID
	case changelog.DomainEntry != nil:
		recordID, err := e.appendSnapshotRecord(ctx, metadata.SnapshotSyncTypeDomainEntryProjection, changelog.DomainEntry.ID, metadata.SnapshotActionUpsert)
		if err != nil {
			return err
		}
		changelog.SnapshotRecordID = recordID
	default:
		return nil
	}
	e.Hub.PublishAll(changelog)
	log.Printf("replication: publish changelog done node_id=%s snapshot_record_id=%d", changelog.NodeID, changelog.SnapshotRecordID)
	return nil
}

func (e *Engine) BuildSnapshot(ctx context.Context, nodeID string) (*enginepkg.Snapshot, error) {
	log.Printf("replication: build snapshot begin node_id=%s", nodeID)
	snapshot := &enginepkg.Snapshot{
		NodeID:    nodeID,
		CreatedAt: time.Now().UTC(),
	}
	if e == nil || e.Repo == nil {
		return snapshot, nil
	}
	if err := e.Repo.DNSRecords().ListResource(ctx, &snapshot.DNSRecords, "fqdn asc, record_type asc, id asc"); err != nil {
		log.Printf("replication: list dns records failed node_id=%s err=%v", nodeID, err)
		return nil, err
	}
	var domains []metadata.DomainEndpoint
	if err := e.Repo.DomainEndpoints().ListResource(ctx, &domains, "hostname asc"); err != nil {
		log.Printf("replication: list domain endpoints failed node_id=%s err=%v", nodeID, err)
		return nil, err
	}
	for i := range domains {
		entry, err := e.Repo.GetDomainEntryProjectionByDomain(ctx, domains[i].Hostname)
		if err == nil && entry != nil {
			snapshot.DomainEntries = append(snapshot.DomainEntries, *entry)
		}
	}
	log.Printf("replication: build snapshot done node_id=%s dns=%d domains=%d", nodeID, len(snapshot.DNSRecords), len(snapshot.DomainEntries))
	return snapshot, nil
}

func (e *Engine) appendSnapshotRecord(ctx context.Context, syncType metadata.SnapshotSyncType, syncID string, action metadata.SnapshotAction) (uint64, error) {
	record := &metadata.SnapshotRecord{SyncType: syncType, SyncID: syncID, Action: action}
	if err := e.Repo.AppendSnapshotRecord(ctx, record); err != nil {
		log.Printf("replication: append snapshot record failed type=%s sync_id=%s action=%s err=%v", syncType, syncID, action, err)
		return 0, err
	}
	log.Printf("replication: append snapshot record done id=%d type=%s sync_id=%s action=%s", record.ID, syncType, syncID, action)
	return record.ID, nil
}

func (e *Engine) GetSnapshot(req *replpb.SnapshotRequest, stream grpc.ServerStreamingServer[replpb.Snapshot]) error {
	log.Printf("replication: get snapshot begin node_id=%s after_record_id=%d", req.GetNodeId(), req.GetSnapshotRecordId())
	records, err := e.Repo.ListSnapshotRecordsAfter(stream.Context(), req.GetSnapshotRecordId())
	if err != nil {
		log.Printf("replication: get snapshot list records failed node_id=%s err=%v", req.GetNodeId(), err)
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
			log.Printf("replication: send snapshot chunk failed node_id=%s last=%v snapshot_record_id=%d dns=%d domains=%d err=%v", req.GetNodeId(), last, lastSeen, len(chunk.DNSRecords), len(chunk.DomainEntries), err)
			return err
		}
		log.Printf("replication: send snapshot chunk done node_id=%s last=%v snapshot_record_id=%d dns=%d domains=%d", req.GetNodeId(), last, lastSeen, len(chunk.DNSRecords), len(chunk.DomainEntries))
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
	log.Printf("replication: get snapshot finished node_id=%s records=%d", req.GetNodeId(), len(records))
	return sendChunk(true)
}

func (e *Engine) Subscribe(req *replpb.SubscriptionRequest, stream grpc.ServerStreamingServer[replpb.ChangeNotification]) error {
	nodeID := req.GetNodeId()
	log.Printf("replication: subscribe begin node_id=%s", nodeID)
	subID, ch := e.Hub.Subscribe(nodeID, 128)
	defer e.Hub.Unsubscribe(nodeID, subID)
	log.Printf("replication: subscribe registered node_id=%s sub_id=%d", nodeID, subID)
	for {
		select {
		case <-stream.Context().Done():
			log.Printf("replication: subscribe context done node_id=%s sub_id=%d err=%v", nodeID, subID, stream.Context().Err())
			return stream.Context().Err()
		case notice, ok := <-ch:
			if !ok {
				log.Printf("replication: subscribe channel closed node_id=%s sub_id=%d", nodeID, subID)
				return nil
			}
			if err := stream.Send(enginepkg.ChangeNotificationToProto(notice)); err != nil {
				log.Printf("replication: subscribe send failed node_id=%s sub_id=%d snapshot_record_id=%d err=%v", nodeID, subID, notice.SnapshotRecordID, err)
				return err
			}
			log.Printf("replication: subscribe sent node_id=%s sub_id=%d snapshot_record_id=%d dns=%v domain=%v", nodeID, subID, notice.SnapshotRecordID, notice.DNSRecord != nil, notice.DomainEntry != nil)
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

func (e *Engine) Stop() error {
	return e.shutdown()
}

func (e *Engine) shutdown() error {
	var firstErr error
	shutdownCtx, cancel := e.shutdownContext()
	defer cancel()
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
		if err := e.httpServer.Shutdown(shutdownCtx); err != nil && err != context.Canceled && firstErr == nil {
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
		case <-shutdownCtx.Done():
			e.grpcServer.Stop()
			if shutdownCtx.Err() != nil && shutdownCtx.Err() != context.Canceled && firstErr == nil {
				firstErr = shutdownCtx.Err()
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

func (e *Engine) shutdownContext() (context.Context, context.CancelFunc) {
	timeout := e.Config.ShutdownTimeout
	if timeout <= 0 {
		timeout = 5 * time.Second
	}
	return context.WithTimeout(context.Background(), timeout)
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
