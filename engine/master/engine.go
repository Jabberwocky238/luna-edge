package master

import (
	"context"
	"fmt"
	"log"
	"net"
	"net/http"
	"time"

	"slices"

	"github.com/jabberwocky238/luna-edge/engine/master/acme"
	masterk8s "github.com/jabberwocky238/luna-edge/engine/master/k8s_bridge"
	"github.com/jabberwocky238/luna-edge/replication"
	"github.com/jabberwocky238/luna-edge/repository"
	"github.com/jabberwocky238/luna-edge/repository/connection"
	"github.com/jabberwocky238/luna-edge/repository/metadata"
)

const (
	masterColorPrefix = "\033[1;32m[MASTER]\033[0m "
)

func masterLogf(format string, args ...any) {
	log.Printf(masterColorPrefix+format, args...)
}

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
	NODE_ID string
	ctx     context.Context

	Config    *Config
	Factory   repository.Factory
	Repo      repository.Repository
	Hub       *Hub
	API       *API
	Bundles   CertificateBundleProvider
	Certs     *CertReconciler
	K8sBridge *masterk8s.Bridge

	grpcServer *replication.GRPCServer
	httpServer *http.Server
}

type HTTP01Registry interface {
	Set(token, keyAuthorization string)
	Get(token string) (keyAuthorization string, ok bool)
	Delete(token string)
}

func New(nodeID string, cfg *Config) (*Engine, error) {
	if cfg.ShutdownTimeout <= 0 {
		cfg.ShutdownTimeout = 5 * time.Second
	}
	if cfg.StorageDriver == "" {
		cfg.StorageDriver = connection.DriverPostgres
	}
	factory := repository.NewFactory(connection.Config{
		Driver:      cfg.StorageDriver,
		DSN:         cfg.PostgresDSN,
		Path:        cfg.SQLitePath,
		AutoMigrate: cfg.AutoMigrate,
	})
	engine := &Engine{
		NODE_ID: nodeID,
		Config:  cfg,
		Factory: factory,
		Hub:     NewHub(),
	}
	return engine, nil
}

func (e *Engine) Start(ctx context.Context) error {
	e.ctx = ctx
	if err := e.Factory.Start(); err != nil {
		return err
	}
	defer func() {
		if err := e.Factory.Close(); err != nil {
			log.Printf("master: factory stop failed err=%v", err)
		}
	}()
	e.Repo = e.Factory.Repository()
	if e.Bundles == nil {
		bundles, err := NewS3CertificateBundleProvider(e.Repo, e.Config.S3)
		if err != nil {
			return err
		}
		e.Bundles = bundles
	}
	e.Certs = NewCertReconciler(e.Repo, e.Config.ACME, defaultCertReconcileInterval, defaultCertRenewBefore, func(ctx context.Context, fqdn string) error {
		return e.BoardcastDomainEndpointProjection(ctx, fqdn)
	}, e.Bundles)
	e.API = NewAPI(e.Certs.http01Registry)
	if e.Config.K8sBridgeEnabled && e.K8sBridge == nil {
		bridge, err := masterk8s.New(masterk8s.Config{
			Namespace:    e.Config.K8sNamespace,
			IngressClass: e.Config.K8sIngressClass,
			Enabled:      true,
		}, e.Repo, func(ctx context.Context, records []metadata.DNSRecord) error {
			for i := range records {
				if err := e.BoardcastDNSRecord(ctx, records[i].ID); err != nil {
					return err
				}
			}
			return nil
		}, func(ctx context.Context, recordID string) error {
			return e.BoardcastDomainEndpointProjection(ctx, recordID)
		})
		if err != nil {
			return err
		}
		e.K8sBridge = bridge
	}
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
		defer func() {
			if err := e.K8sBridge.Stop(); err != nil {
				log.Printf("master: k8s bridge stop failed err=%v", err)
			}
		}()
	}
	if e.Certs != nil {
		e.Certs.Start(ctx)
		defer e.Certs.Stop()
		log.Printf("master: cert reconciler started")
	}
	if e.Config.ReplicationListenAddr != "" {
		e.grpcServer = replication.NewGRPCServerEasy(
			e.Config.ReplicationListenAddr,
			e.handleGetSnapshot,
			e.handleSubscribe,
			e.handleFetchCertificateBundle,
		)
		if e.grpcServer == nil {
			return fmt.Errorf("failed to create replication gRPC server")
		}
		defer e.grpcServer.Close()
	}
	if e.Config.ManageListenAddr != "" {
		lis, err := net.Listen("tcp", e.Config.ManageListenAddr)
		if err != nil {
			return err
		}
		e.httpServer = &http.Server{Addr: e.Config.ManageListenAddr, Handler: e.API.Handler()}
		log.Printf("master: manage listener ready addr=%s", lis.Addr().String())
		go func() { _ = e.httpServer.Serve(lis) }()
		defer func() {
			e.httpServer.Shutdown(ctx)
		}()
	}
	log.Printf("master: start done")
	<-ctx.Done()
	log.Printf("master: context done err=%v", ctx.Err())
	return ctx.Err()
}

func (e *Engine) handleGetSnapshot(ctx context.Context, nodeID string, snapshotRecordID uint64, send func(*replication.Snapshot) error) error {
	log.Printf("replication: get snapshot begin node_id=%s after_record_id=%d", nodeID, snapshotRecordID)
	records, err := e.Repo.ListSnapshotRecordsAfter(ctx, snapshotRecordID)
	if err != nil {
		log.Printf("replication: get snapshot list records failed node_id=%s err=%v", nodeID, err)
		return err
	}
	chunk := &replication.Snapshot{NodeID: nodeID, CreatedAt: time.Now().UTC()}
	count := 0
	lastSeen := snapshotRecordID
	sendChunk := func(last bool) error {
		chunk.Last = last
		chunk.SnapshotRecordID = lastSeen
		if len(chunk.DNSRecords) == 0 && len(chunk.DomainEntries) == 0 && !last {
			return nil
		}
		if err := send(chunk); err != nil {
			log.Printf("replication: send snapshot chunk failed node_id=%s last=%v snapshot_record_id=%d dns=%d domains=%d err=%v", nodeID, last, lastSeen, len(chunk.DNSRecords), len(chunk.DomainEntries), err)
			return err
		}
		log.Printf("replication: send snapshot chunk done node_id=%s last=%v snapshot_record_id=%d dns=%d domains=%d", nodeID, last, lastSeen, len(chunk.DNSRecords), len(chunk.DomainEntries))
		chunk = &replication.Snapshot{NodeID: nodeID, CreatedAt: time.Now().UTC()}
		count = 0
		return nil
	}
	for i := range records {
		record := records[i]
		lastSeen = record.ID
		switch record.SyncType {
		case metadata.SnapshotSyncTypeDNSRecord:
			item := &metadata.DNSRecord{}
			if err := e.Repo.DNSRecords().GetResourceByField(ctx, item, "id", record.SyncID); err == nil {
				chunk.DNSRecords = append(chunk.DNSRecords, *item)
				count++
			}
		case metadata.SnapshotSyncTypeDomainEntryProjection:
			item, err := e.Repo.GetDomainEntryProjectionByDomain(ctx, record.SyncID)
			if err == nil && item != nil {
				chunk.DomainEntries = append(chunk.DomainEntries, *item)
				count++
			}
		}
		if count >= 1000 {
			if err := sendChunk(false); err != nil {
				return err
			}
		}
	}
	log.Printf("replication: get snapshot finished node_id=%s records=%d", nodeID, len(records))
	return sendChunk(true)
}

func (e *Engine) handleSubscribe(ctx context.Context, nodeID string, send func(*replication.ChangeNotification) error) error {
	log.Printf("replication: subscribe begin node_id=%s", nodeID)
	subID, ch := e.Hub.Subscribe(nodeID, 128)
	defer e.Hub.Unsubscribe(nodeID, subID)
	log.Printf("replication: subscribe registered node_id=%s sub_id=%d", nodeID, subID)
	for {
		select {
		case <-ctx.Done():
			log.Printf("replication: subscribe context done node_id=%s sub_id=%d err=%v", nodeID, subID, ctx.Err())
			return ctx.Err()
		case notice, ok := <-ch:
			if !ok {
				log.Printf("replication: subscribe channel closed node_id=%s sub_id=%d", nodeID, subID)
				return nil
			}
			if err := send(&replication.ChangeNotification{
				NodeID:           notice.NodeID,
				CreatedAt:        notice.CreatedAt,
				SnapshotRecordID: notice.SnapshotRecordID,
				DNSRecord:        notice.DNSRecord,
				DomainEntry:      notice.DomainEntry,
			}); err != nil {
				log.Printf("replication: subscribe send failed node_id=%s sub_id=%d snapshot_record_id=%d err=%v", nodeID, subID, notice.SnapshotRecordID, err)
				return err
			}
			log.Printf("replication: subscribe sent node_id=%s sub_id=%d snapshot_record_id=%d dns=%v domain=%v", nodeID, subID, notice.SnapshotRecordID, notice.DNSRecord != nil, notice.DomainEntry != nil)
		}
	}
}

func (e *Engine) handleFetchCertificateBundle(ctx context.Context, hostname string, revision uint64) (*replication.CertificateBundle, error) {
	if e == nil || e.Bundles == nil {
		return nil, fmt.Errorf("certificate bundle provider is not configured")
	}
	bundle, err := e.Bundles.FetchCertificateBundle(ctx, hostname, revision)
	if err != nil {
		return nil, err
	}
	if bundle == nil {
		return nil, fmt.Errorf("certificate bundle not found")
	}
	return &replication.CertificateBundle{
		Hostname:     bundle.Hostname,
		Revision:     bundle.Revision,
		TLSCrt:       slices.Clone(bundle.TLSCrt),
		TLSKey:       slices.Clone(bundle.TLSKey),
		MetadataJSON: slices.Clone(bundle.MetadataJSON),
	}, nil
}
