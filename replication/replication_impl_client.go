package replication

import (
	"context"
	"errors"
	"fmt"
	"io"
	"log"

	"github.com/jabberwocky238/luna-edge/replication/replpb"
	"google.golang.org/grpc"
	"google.golang.org/grpc/credentials/insecure"
)

const (
	replicationClientColorPrefix = "\033[1;31m[REPLICATION CLIENT]\033[0m "
)

func replicationClientLogf(format string, args ...any) {
	log.Printf(replicationClientColorPrefix+format, args...)
}

type GRPCClient struct {
	client replpb.ReplicationServiceClient
	closer func() error
}

func NewGRPCClientEasy(masterAddress string) *GRPCClient {
	conn, err := grpc.NewClient(masterAddress, grpc.WithTransportCredentials(insecure.NewCredentials()))
	if err != nil {
		return nil
	}
	return &GRPCClient{client: replpb.NewReplicationServiceClient(conn), closer: func() error { return conn.Close() }}
}

func NewGRPCClient(conn grpc.ClientConnInterface) *GRPCClient {
	return &GRPCClient{client: replpb.NewReplicationServiceClient(conn)}
}

func (c *GRPCClient) GetSnapshot(ctx context.Context, nodeID string, snapshotRecordID uint64) (SnapshotStream, error) {
	replicationClientLogf("replication-client: get snapshot request node_id=%s after_record_id=%d", nodeID, snapshotRecordID)
	stream, err := c.client.GetSnapshot(ctx, &replpb.SnapshotRequest{NodeId: nodeID, SnapshotRecordId: snapshotRecordID})
	if err != nil {
		replicationClientLogf("replication-client: get snapshot request failed node_id=%s after_record_id=%d err=%v", nodeID, snapshotRecordID, err)
		return nil, err
	}
	replicationClientLogf("replication-client: get snapshot stream opened node_id=%s after_record_id=%d", nodeID, snapshotRecordID)
	return grpcSnapshotStream{stream: stream}, nil
}

func (c *GRPCClient) Subscribe(ctx context.Context, nodeID string) (NoticeStream, error) {
	replicationClientLogf("replication-client: subscribe request node_id=%s", nodeID)
	stream, err := c.client.Subscribe(ctx, &replpb.SubscriptionRequest{NodeId: nodeID})
	if err != nil {
		replicationClientLogf("replication-client: subscribe request failed node_id=%s err=%v", nodeID, err)
		return nil, err
	}
	replicationClientLogf("replication-client: subscribe stream opened node_id=%s", nodeID)
	return grpcNoticeStream{stream: stream}, nil
}

func (c *GRPCClient) FetchCertificateBundle(ctx context.Context, hostname string, revision uint64) (*CertificateBundle, error) {
	replicationClientLogf("replication-client: fetch certificate bundle request hostname=%s revision=%d", hostname, revision)
	resp, err := c.client.FetchCertificateBundle(ctx, &replpb.CertificateBundleRequest{Hostname: hostname, Revision: revision})
	if err != nil {
		replicationClientLogf("replication-client: fetch certificate bundle failed hostname=%s revision=%d err=%v", hostname, revision, err)
		return nil, err
	}
	if resp == nil {
		return nil, fmt.Errorf("certificate bundle response is nil")
	}
	replicationClientLogf("replication-client: fetch certificate bundle done hostname=%s revision=%d crt_bytes=%d key_bytes=%d", resp.GetHostname(), resp.GetRevision(), len(resp.GetTlsCrt()), len(resp.GetTlsKey()))
	return &CertificateBundle{Hostname: resp.GetHostname(), Revision: resp.GetRevision(), TLSCrt: append([]byte(nil), resp.GetTlsCrt()...), TLSKey: append([]byte(nil), resp.GetTlsKey()...), MetadataJSON: append([]byte(nil), resp.GetMetadataJson()...)}, nil
}

func (c *GRPCClient) Close() error {
	if c.closer != nil {
		c.closer()
	}
	return nil
}

type grpcSnapshotStream struct {
	stream replpb.ReplicationService_GetSnapshotClient
}

func (s grpcSnapshotStream) Recv() (*Snapshot, error) {
	msg, err := s.stream.Recv()
	if err != nil {
		if err != io.EOF {
			replicationClientLogf("replication-client: recv snapshot failed err=%v", err)
		}
		return nil, err
	}
	out := SnapshotFromProto(msg)
	if out != nil {
		replicationClientLogf("replication-client: recv snapshot node_id=%s snapshot_record_id=%d last=%v dns=%d domains=%d", out.NodeID, out.SnapshotRecordID, out.Last, len(out.DNSRecords), len(out.DomainEntries))
	}
	return out, nil
}

type grpcNoticeStream struct {
	stream replpb.ReplicationService_SubscribeClient
}

func (s grpcNoticeStream) Recv() (*ChangeNotification, error) {
	msg, err := s.stream.Recv()
	if err != nil {
		if err != io.EOF && !errors.Is(err, context.Canceled) {
			replicationClientLogf("replication-client: recv change notice failed err=%v", err)
		}
		return nil, err
	}
	out := ChangeNotificationFromProto(msg)
	if out != nil {
		replicationClientLogf("replication-client: recv change notice snapshot_record_id=%d dns=%v domain=%v", out.SnapshotRecordID, out.DNSRecord != nil, out.DomainEntry != nil)
	}
	return out, nil
}
