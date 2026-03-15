package master

import (
	"context"
	"fmt"
	"net"
	"net/http"
	"time"

	enginepkg "github.com/jabberwocky238/luna-edge/engine"
	"github.com/jabberwocky238/luna-edge/engine/master/manage"
	"github.com/jabberwocky238/luna-edge/replication/replpb"
	"github.com/jabberwocky238/luna-edge/repository"
	"github.com/jabberwocky238/luna-edge/repository/connection"
	"google.golang.org/grpc"
	"slices"
)

type Config struct {
	StorageDriver         connection.Driver
	SQLitePath            string
	PostgresDSN           string
	AutoMigrate           bool
	S3                    S3Config
	ReplicationListenAddr string
	ManageListenAddr      string
	ShutdownTimeout       time.Duration
}

type Engine struct {
	replpb.UnimplementedReplicationServiceServer

	Config  Config
	Factory repository.Factory
	Repo    repository.Repository
	Hub     *Hub
	Builder enginepkg.ProjectionBuilder
	Bundles CertificateBundleProvider
	Manage  *manage.API

	grpcServer   *grpc.Server
	grpcListener net.Listener
	httpServer   *http.Server
	httpListener net.Listener
}

type CertificateBundleProvider interface {
	FetchCertificateBundle(ctx context.Context, hostname string, revision uint64) (*enginepkg.CertificateBundle, error)
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
	builder, err := enginepkg.NewRepositoryProjectionBuilder(repo)
	if err != nil {
		_ = factory.Close()
		return nil, err
	}

	engine := &Engine{
		Config:  cfg,
		Factory: factory,
		Repo:    repo,
		Hub:     NewHub(),
		Builder: builder,
	}
	if cfg.S3.Enabled() {
		bundles, err := NewS3CertificateBundleProvider(repo, cfg.S3)
		if err != nil {
			_ = factory.Close()
			return nil, err
		}
		engine.Bundles = bundles
	}
	wrapper := manage.NewWrapper(repo, builder, engine)
	engine.Manage = manage.NewAPI(wrapper)
	return engine, nil
}

func (e *Engine) Start() error {
	if e.Config.ReplicationListenAddr != "" {
		lis, err := net.Listen("tcp", e.Config.ReplicationListenAddr)
		if err != nil {
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
	e.Hub.Publish(snapshot.NodeID, &enginepkg.ChangeNotification{
		NodeID:    snapshot.NodeID,
		Versions:  snapshot.Versions,
		CreatedAt: time.Now().UTC(),
	})
	return nil
}

func (e *Engine) PublishNode(ctx context.Context, nodeID string) error {
	if e == nil || e.Hub == nil {
		return nil
	}
	versions, err := e.currentVersions(ctx, nodeID)
	if err != nil {
		return err
	}
	e.Hub.Publish(nodeID, &enginepkg.ChangeNotification{
		NodeID:    nodeID,
		Versions:  versions,
		CreatedAt: time.Now().UTC(),
	})
	return nil
}

func (e *Engine) BuildSnapshot(ctx context.Context, nodeID string) (*enginepkg.Snapshot, error) {
	snapshot := &enginepkg.Snapshot{
		NodeID:    nodeID,
		CreatedAt: time.Now().UTC(),
	}
	if e == nil || e.Repo == nil || e.Builder == nil {
		return snapshot, nil
	}

	attachments, err := e.Repo.ListAttachmentsByNodeID(ctx, nodeID)
	if err != nil {
		return nil, err
	}
	for _, attachment := range attachments {
		if assignment, err := e.Builder.BuildAssignmentRecord(ctx, &attachment); err == nil && assignment != nil {
			snapshot.Assignments = append(snapshot.Assignments, *assignment)
			accumulateAssignmentVersions(&snapshot.Versions, *assignment)
		}
		if route, err := e.Builder.BuildRouteRecord(ctx, attachment.DomainID); err == nil && route != nil {
			snapshot.Routes = append(snapshot.Routes, *route)
			accumulateRouteVersions(&snapshot.Versions, *route)
		}
		if binding, err := e.Builder.BuildBindingRecord(ctx, attachment.DomainID); err == nil && binding != nil {
			snapshot.Bindings = append(snapshot.Bindings, *binding)
			accumulateBindingVersions(&snapshot.Versions, *binding)
		}
		if cert, err := e.Builder.BuildCertificateRecord(ctx, attachment.DomainID, attachment.DesiredCertificateRevision); err == nil && cert != nil {
			snapshot.Certificates = append(snapshot.Certificates, *cert)
			accumulateCertificateVersions(&snapshot.Versions, *cert)
		}
	}
	return snapshot, nil
}

func (e *Engine) currentVersions(ctx context.Context, nodeID string) (enginepkg.VersionVector, error) {
	snapshot, err := e.BuildSnapshot(ctx, nodeID)
	if err != nil {
		return enginepkg.VersionVector{}, err
	}
	if snapshot == nil {
		return enginepkg.VersionVector{}, nil
	}
	return snapshot.Versions, nil
}

func accumulateAssignmentVersions(out *enginepkg.VersionVector, assignment enginepkg.AssignmentRecord) {
	if assignment.DesiredRouteVersion > out.DesiredRouteVersion {
		out.DesiredRouteVersion = assignment.DesiredRouteVersion
	}
	if assignment.DesiredCertificateRevision > out.DesiredCertificateRevision {
		out.DesiredCertificateRevision = assignment.DesiredCertificateRevision
	}
	if assignment.DesiredDNSVersion > out.DesiredDNSVersion {
		out.DesiredDNSVersion = assignment.DesiredDNSVersion
	}
}

func accumulateRouteVersions(out *enginepkg.VersionVector, route enginepkg.RouteRecord) {
	if route.RouteVersion > out.DesiredRouteVersion {
		out.DesiredRouteVersion = route.RouteVersion
	}
	if route.CertificateRevision > out.DesiredCertificateRevision {
		out.DesiredCertificateRevision = route.CertificateRevision
	}
}

func accumulateBindingVersions(out *enginepkg.VersionVector, binding enginepkg.BindingRecord) {
	if binding.RouteVersion > out.DesiredRouteVersion {
		out.DesiredRouteVersion = binding.RouteVersion
	}
}

func accumulateCertificateVersions(out *enginepkg.VersionVector, cert enginepkg.CertificateRecord) {
	if cert.Revision > out.DesiredCertificateRevision {
		out.DesiredCertificateRevision = cert.Revision
	}
}

func (e *Engine) GetSnapshot(ctx context.Context, req *replpb.SnapshotRequest) (*replpb.Snapshot, error) {
	snapshot, err := e.BuildSnapshot(ctx, req.GetNodeId())
	if err != nil {
		return nil, err
	}
	return enginepkg.SnapshotToProto(snapshot), nil
}

func (e *Engine) Subscribe(req *replpb.SubscriptionRequest, stream grpc.ServerStreamingServer[replpb.ChangeNotification]) error {
	nodeID := req.GetNodeId()
	current, err := e.currentVersions(stream.Context(), nodeID)
	if err != nil {
		return err
	}
	known := enginepkg.VersionVectorFromProto(req.GetKnownVersions())
	if current.DiffersFrom(known) {
		if err := stream.Send(enginepkg.ChangeNotificationToProto(&enginepkg.ChangeNotification{
			NodeID:    nodeID,
			Versions:  current,
			CreatedAt: time.Now().UTC(),
		})); err != nil {
			return err
		}
	}

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
