package replication

import (
	"context"
	"errors"
	"fmt"
	"io"
	"log"
	"net"

	"github.com/jabberwocky238/luna-edge/replication/replpb"
	"google.golang.org/grpc"
)

const (
	replicationServerColorPrefix = "\033[1;31m[REPLICATION SERVER]\033[0m "
)

func replicationServerLogf(format string, args ...any) {
	log.Printf(replicationServerColorPrefix+format, args...)
}

type GetSnapshotHandler func(ctx context.Context, nodeID string, snapshotRecordID uint64, send func(*Snapshot) error) error
type SubscribeHandler func(ctx context.Context, nodeID string, send func(*ChangeNotification) error) error
type FetchCertificateBundleHandler func(ctx context.Context, hostname string, revision uint64) (*CertificateBundle, error)

type GRPCServer struct {
	replpb.UnimplementedReplicationServiceServer

	listenAddr                    string
	grpcServer                    *grpc.Server
	getSnapshotHandler            GetSnapshotHandler
	subscribeHandler              SubscribeHandler
	fetchCertificateBundleHandler FetchCertificateBundleHandler
	closer                        func() error
}

func NewGRPCServerEasy(listenAddr string, getSnapshotHandler GetSnapshotHandler, subscribeHandler SubscribeHandler, fetchCertificateBundleHandler FetchCertificateBundleHandler, opts ...grpc.ServerOption) *GRPCServer {
	server := &GRPCServer{
		listenAddr:                    listenAddr,
		getSnapshotHandler:            getSnapshotHandler,
		subscribeHandler:              subscribeHandler,
		fetchCertificateBundleHandler: fetchCertificateBundleHandler,
		grpcServer:                    grpc.NewServer(opts...),
	}
	lis, err := net.Listen("tcp", server.listenAddr)
	if err != nil {
		return nil
	}
	replpb.RegisterReplicationServiceServer(server.grpcServer, server)
	replicationServerLogf("replication-server: listener ready addr=%s", lis.Addr().String())
	go func() { _ = server.grpcServer.Serve(lis) }()
	server.closer = func() error {
		server.grpcServer.GracefulStop()
		return nil
	}
	return server
}

func NewGRPCServer(injectServer replpb.ReplicationServiceServer, getSnapshotHandler GetSnapshotHandler, subscribeHandler SubscribeHandler, fetchCertificateBundleHandler FetchCertificateBundleHandler, opts ...grpc.ServerOption) *GRPCServer {
	server := &GRPCServer{
		listenAddr:                    "",
		getSnapshotHandler:            getSnapshotHandler,
		subscribeHandler:              subscribeHandler,
		fetchCertificateBundleHandler: fetchCertificateBundleHandler,
		grpcServer:                    grpc.NewServer(opts...),
	}
	replpb.RegisterReplicationServiceServer(server.grpcServer, injectServer)
	server.closer = func() error {
		server.grpcServer.GracefulStop()
		return nil
	}

	return server
}

func (s *GRPCServer) GetSnapshot(req *replpb.SnapshotRequest, stream grpc.ServerStreamingServer[replpb.Snapshot]) error {
	if s == nil || s.getSnapshotHandler == nil {
		return fmt.Errorf("get snapshot handler is not configured")
	}
	nodeID := req.GetNodeId()
	snapshotRecordID := req.GetSnapshotRecordId()
	replicationServerLogf("replication-server: get snapshot request node_id=%s after_record_id=%d", nodeID, snapshotRecordID)
	err := s.getSnapshotHandler(stream.Context(), nodeID, snapshotRecordID, func(snapshot *Snapshot) error {
		if snapshot == nil {
			return nil
		}
		if err := stream.Send(SnapshotToProto(snapshot)); err != nil {
			replicationServerLogf("replication-server: get snapshot send failed node_id=%s snapshot_record_id=%d last=%v err=%v", nodeID, snapshot.SnapshotRecordID, snapshot.Last, err)
			return err
		}
		replicationServerLogf("replication-server: get snapshot sent node_id=%s snapshot_record_id=%d last=%v dns=%d domains=%d", snapshot.NodeID, snapshot.SnapshotRecordID, snapshot.Last, len(snapshot.DNSRecords), len(snapshot.DomainEntries))
		return nil
	})
	if err != nil {
		replicationServerLogf("replication-server: get snapshot failed node_id=%s after_record_id=%d err=%v", nodeID, snapshotRecordID, err)
		return err
	}
	replicationServerLogf("replication-server: get snapshot done node_id=%s after_record_id=%d", nodeID, snapshotRecordID)
	return nil
}

func (s *GRPCServer) Subscribe(req *replpb.SubscriptionRequest, stream grpc.ServerStreamingServer[replpb.ChangeNotification]) error {
	if s == nil || s.subscribeHandler == nil {
		return fmt.Errorf("subscribe handler is not configured")
	}
	nodeID := req.GetNodeId()
	replicationServerLogf("replication-server: subscribe request node_id=%s", nodeID)
	err := s.subscribeHandler(stream.Context(), nodeID, func(notice *ChangeNotification) error {
		if notice == nil {
			return nil
		}
		if err := stream.Send(ChangeNotificationToProto(notice)); err != nil {
			replicationServerLogf("replication-server: subscribe send failed node_id=%s snapshot_record_id=%d err=%v", nodeID, notice.SnapshotRecordID, err)
			return err
		}
		replicationServerLogf("replication-server: subscribe sent node_id=%s snapshot_record_id=%d dns=%v domain=%v", notice.NodeID, notice.SnapshotRecordID, notice.DNSRecord != nil, notice.DomainEntry != nil)
		return nil
	})
	if err != nil {
		if errors.Is(err, context.Canceled) || errors.Is(err, io.EOF) {
			replicationServerLogf("replication-server: subscribe closed node_id=%s err=%v", nodeID, err)
			return nil
		}
		replicationServerLogf("replication-server: subscribe failed node_id=%s err=%v", nodeID, err)
		return err
	}
	replicationServerLogf("replication-server: subscribe done node_id=%s", nodeID)
	return nil
}

func (s *GRPCServer) FetchCertificateBundle(ctx context.Context, req *replpb.CertificateBundleRequest) (*replpb.CertificateBundleResponse, error) {
	if s == nil || s.fetchCertificateBundleHandler == nil {
		return nil, fmt.Errorf("fetch certificate bundle handler is not configured")
	}
	hostname := req.GetHostname()
	revision := req.GetRevision()
	replicationServerLogf("replication-server: fetch certificate bundle request hostname=%s revision=%d", hostname, revision)
	bundle, err := s.fetchCertificateBundleHandler(ctx, hostname, revision)
	if err != nil {
		replicationServerLogf("replication-server: fetch certificate bundle failed hostname=%s revision=%d err=%v", hostname, revision, err)
		return nil, err
	}
	if bundle == nil {
		return nil, fmt.Errorf("certificate bundle not found")
	}
	replicationServerLogf("replication-server: fetch certificate bundle done hostname=%s revision=%d crt_bytes=%d key_bytes=%d", bundle.Hostname, bundle.Revision, len(bundle.TLSCrt), len(bundle.TLSKey))
	return &replpb.CertificateBundleResponse{
		Hostname:     bundle.Hostname,
		Revision:     bundle.Revision,
		TlsCrt:       append([]byte(nil), bundle.TLSCrt...),
		TlsKey:       append([]byte(nil), bundle.TLSKey...),
		MetadataJson: append([]byte(nil), bundle.MetadataJSON...),
	}, nil
}

func (s *GRPCServer) Close() error {
	if s != nil && s.closer != nil {
		return s.closer()
	}
	return nil
}
