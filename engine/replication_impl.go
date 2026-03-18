package engine

import (
	"context"
	"fmt"
	"io"
	"log"

	"github.com/jabberwocky238/luna-edge/replication/replpb"
	"google.golang.org/grpc"
	"google.golang.org/grpc/credentials/insecure"
)

type GRPCClient struct {
	client replpb.ReplicationServiceClient
	closer func() error
}

func NewGRPCClientEasy(masterAddress string) *GRPCClient {
	conn, err := grpc.NewClient(masterAddress, grpc.WithTransportCredentials(insecure.NewCredentials()))
	if err != nil {
		log.Fatalf("failed to connect to master: %v", err)
	}
	return &GRPCClient{client: replpb.NewReplicationServiceClient(conn), closer: func() error { return conn.Close() }}
}

func NewGRPCClient(conn grpc.ClientConnInterface) *GRPCClient {
	return &GRPCClient{client: replpb.NewReplicationServiceClient(conn)}
}

func (c *GRPCClient) GetSnapshot(ctx context.Context, nodeID string, snapshotRecordID uint64) (SnapshotStream, error) {
	log.Printf("replication-client: get snapshot request node_id=%s after_record_id=%d", nodeID, snapshotRecordID)
	stream, err := c.client.GetSnapshot(ctx, &replpb.SnapshotRequest{NodeId: nodeID, SnapshotRecordId: snapshotRecordID})
	if err != nil {
		log.Printf("replication-client: get snapshot request failed node_id=%s after_record_id=%d err=%v", nodeID, snapshotRecordID, err)
		return nil, err
	}
	log.Printf("replication-client: get snapshot stream opened node_id=%s after_record_id=%d", nodeID, snapshotRecordID)
	return grpcSnapshotStream{stream: stream}, nil
}

func (c *GRPCClient) Subscribe(ctx context.Context, nodeID string) (NoticeStream, error) {
	log.Printf("replication-client: subscribe request node_id=%s", nodeID)
	stream, err := c.client.Subscribe(ctx, &replpb.SubscriptionRequest{NodeId: nodeID})
	if err != nil {
		log.Printf("replication-client: subscribe request failed node_id=%s err=%v", nodeID, err)
		return nil, err
	}
	log.Printf("replication-client: subscribe stream opened node_id=%s", nodeID)
	return grpcNoticeStream{stream: stream}, nil
}

func (c *GRPCClient) FetchCertificateBundle(ctx context.Context, hostname string, revision uint64) (*CertificateBundle, error) {
	log.Printf("replication-client: fetch certificate bundle request hostname=%s revision=%d", hostname, revision)
	resp, err := c.client.FetchCertificateBundle(ctx, &replpb.CertificateBundleRequest{Hostname: hostname, Revision: revision})
	if err != nil {
		log.Printf("replication-client: fetch certificate bundle failed hostname=%s revision=%d err=%v", hostname, revision, err)
		return nil, err
	}
	if resp == nil {
		return nil, fmt.Errorf("certificate bundle response is nil")
	}
	log.Printf("replication-client: fetch certificate bundle done hostname=%s revision=%d crt_bytes=%d key_bytes=%d", resp.GetHostname(), resp.GetRevision(), len(resp.GetTlsCrt()), len(resp.GetTlsKey()))
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
			log.Printf("replication-client: recv snapshot failed err=%v", err)
		}
		return nil, err
	}
	out := SnapshotFromProto(msg)
	if out != nil {
		log.Printf("replication-client: recv snapshot node_id=%s snapshot_record_id=%d last=%v dns=%d domains=%d", out.NodeID, out.SnapshotRecordID, out.Last, len(out.DNSRecords), len(out.DomainEntries))
	}
	return out, nil
}

type grpcNoticeStream struct {
	stream replpb.ReplicationService_SubscribeClient
}

func (s grpcNoticeStream) Recv() (*ChangeNotification, error) {
	msg, err := s.stream.Recv()
	if err != nil {
		if err != io.EOF {
			log.Printf("replication-client: recv change notice failed err=%v", err)
		}
		return nil, err
	}
	out := ChangeNotificationFromProto(msg)
	if out != nil {
		log.Printf("replication-client: recv change notice snapshot_record_id=%d dns=%v domain=%v", out.SnapshotRecordID, out.DNSRecord != nil, out.DomainEntry != nil)
	}
	return out, nil
}
