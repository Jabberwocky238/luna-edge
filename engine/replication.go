package engine

import (
	"context"
	"fmt"
	"io"
	"time"

	"github.com/golang/protobuf/ptypes/timestamp"
	"github.com/jabberwocky238/luna-edge/replication/replpb"
	"github.com/jabberwocky238/luna-edge/repository/metadata"
	"google.golang.org/grpc"
)

type CertificateBundle struct {
	Hostname     string
	Revision     uint64
	TLSCrt       []byte
	TLSKey       []byte
	MetadataJSON []byte
}

type Snapshot struct {
	NodeID           string
	CreatedAt        time.Time
	SnapshotRecordID uint64
	Last             bool
	DNSRecords       []metadata.DNSRecord
	DomainEntries    []metadata.DomainEntryProjection
}

type ChangeNotification struct {
	NodeID           string
	CreatedAt        time.Time
	SnapshotRecordID uint64
	DNSRecord        *metadata.DNSRecord
	DomainEntry      *metadata.DomainEntryProjection
}

type NoticeStream interface {
	Recv() (*ChangeNotification, error)
}

type SnapshotStream interface {
	Recv() (*Snapshot, error)
}

type Client interface {
	GetSnapshot(ctx context.Context, nodeID string, snapshotRecordID uint64) (SnapshotStream, error)
	Subscribe(ctx context.Context, nodeID string) (NoticeStream, error)
	FetchCertificateBundle(ctx context.Context, hostname string, revision uint64) (*CertificateBundle, error)
}

type Subscriber interface {
	Subscribe(ctx context.Context, nodeID string) error
}

type SnapshotApplier interface {
	ApplySnapshot(ctx context.Context, snapshot *Snapshot) error
}

type Publisher interface {
	PublishSnapshot(ctx context.Context, snapshot *Snapshot) error
	PublishNode(ctx context.Context, nodeID string) error
}

type GRPCClient struct {
	client replpb.ReplicationServiceClient
}

func NewGRPCClient(conn grpc.ClientConnInterface) *GRPCClient {
	return &GRPCClient{client: replpb.NewReplicationServiceClient(conn)}
}

func (c *GRPCClient) GetSnapshot(ctx context.Context, nodeID string, snapshotRecordID uint64) (SnapshotStream, error) {
	stream, err := c.client.GetSnapshot(ctx, &replpb.SnapshotRequest{NodeId: nodeID, SnapshotRecordId: snapshotRecordID})
	if err != nil {
		return nil, err
	}
	return grpcSnapshotStream{stream: stream}, nil
}

func (c *GRPCClient) Subscribe(ctx context.Context, nodeID string) (NoticeStream, error) {
	stream, err := c.client.Subscribe(ctx, &replpb.SubscriptionRequest{NodeId: nodeID})
	if err != nil {
		return nil, err
	}
	return grpcNoticeStream{stream: stream}, nil
}

func (c *GRPCClient) FetchCertificateBundle(ctx context.Context, hostname string, revision uint64) (*CertificateBundle, error) {
	resp, err := c.client.FetchCertificateBundle(ctx, &replpb.CertificateBundleRequest{Hostname: hostname, Revision: revision})
	if err != nil {
		return nil, err
	}
	if resp == nil {
		return nil, fmt.Errorf("certificate bundle response is nil")
	}
	return &CertificateBundle{Hostname: resp.GetHostname(), Revision: resp.GetRevision(), TLSCrt: append([]byte(nil), resp.GetTlsCrt()...), TLSKey: append([]byte(nil), resp.GetTlsKey()...), MetadataJSON: append([]byte(nil), resp.GetMetadataJson()...)}, nil
}

type grpcSnapshotStream struct {
	stream replpb.ReplicationService_GetSnapshotClient
}

func (s grpcSnapshotStream) Recv() (*Snapshot, error) {
	msg, err := s.stream.Recv()
	if err != nil {
		return nil, err
	}
	return SnapshotFromProto(msg), nil
}

type grpcNoticeStream struct {
	stream replpb.ReplicationService_SubscribeClient
}

func (s grpcNoticeStream) Recv() (*ChangeNotification, error) {
	msg, err := s.stream.Recv()
	if err != nil {
		return nil, err
	}
	return ChangeNotificationFromProto(msg), nil
}

func SnapshotToProto(in *Snapshot) *replpb.Snapshot {
	if in == nil {
		return nil
	}
	out := &replpb.Snapshot{NodeId: in.NodeID, CreatedAt: timeToProto(in.CreatedAt), SnapshotRecordId: in.SnapshotRecordID, Last: in.Last}
	out.DnsRecords = make([]*replpb.DNSRecord, 0, len(in.DNSRecords))
	for i := range in.DNSRecords {
		out.DnsRecords = append(out.DnsRecords, dnsRecordToProto(in.DNSRecords[i]))
	}
	out.DomainEntries = make([]*replpb.DomainEntryProjection, 0, len(in.DomainEntries))
	for i := range in.DomainEntries {
		out.DomainEntries = append(out.DomainEntries, domainEntryProjectionToProto(in.DomainEntries[i]))
	}
	return out
}

func SnapshotFromProto(in *replpb.Snapshot) *Snapshot {
	if in == nil {
		return nil
	}
	out := &Snapshot{NodeID: in.GetNodeId(), CreatedAt: timeFromProto(in.GetCreatedAt()), SnapshotRecordID: in.GetSnapshotRecordId(), Last: in.GetLast()}
	out.DNSRecords = make([]metadata.DNSRecord, 0, len(in.GetDnsRecords()))
	for _, item := range in.GetDnsRecords() {
		out.DNSRecords = append(out.DNSRecords, dnsRecordFromProto(item))
	}
	out.DomainEntries = make([]metadata.DomainEntryProjection, 0, len(in.GetDomainEntries()))
	for _, item := range in.GetDomainEntries() {
		out.DomainEntries = append(out.DomainEntries, domainEntryProjectionFromProto(item))
	}
	return out
}

func ChangeNotificationToProto(in *ChangeNotification) *replpb.ChangeNotification {
	if in == nil {
		return nil
	}
	out := &replpb.ChangeNotification{NodeId: in.NodeID, CreatedAt: timeToProto(in.CreatedAt), SnapshotRecordId: in.SnapshotRecordID}
	if in.DNSRecord != nil {
		out.Entry = &replpb.ChangeNotification_DnsRecord{DnsRecord: dnsRecordToProto(*in.DNSRecord)}
	}
	if in.DomainEntry != nil {
		out.Entry = &replpb.ChangeNotification_DomainEntry{DomainEntry: domainEntryProjectionToProto(*in.DomainEntry)}
	}
	return out
}

func ChangeNotificationFromProto(in *replpb.ChangeNotification) *ChangeNotification {
	if in == nil {
		return nil
	}
	out := &ChangeNotification{NodeID: in.GetNodeId(), CreatedAt: timeFromProto(in.GetCreatedAt()), SnapshotRecordID: in.GetSnapshotRecordId()}
	if item := in.GetDnsRecord(); item != nil {
		rec := dnsRecordFromProto(item)
		out.DNSRecord = &rec
	}
	if item := in.GetDomainEntry(); item != nil {
		entry := domainEntryProjectionFromProto(item)
		out.DomainEntry = &entry
	}
	return out
}

func dnsRecordToProto(in metadata.DNSRecord) *replpb.DNSRecord {
	return &replpb.DNSRecord{Id: in.ID, Fqdn: in.FQDN, RecordType: string(in.RecordType), RoutingClass: string(in.RoutingClass), TtlSeconds: in.TTLSeconds, ValuesJson: in.ValuesJSON, RoutingKey: in.RoutingKey, Enabled: in.Enabled}
}

func dnsRecordFromProto(in *replpb.DNSRecord) metadata.DNSRecord {
	if in == nil {
		return metadata.DNSRecord{}
	}
	return metadata.DNSRecord{ID: in.GetId(), FQDN: in.GetFqdn(), RecordType: metadata.DNSRecordType(in.GetRecordType()), RoutingClass: metadata.RoutingClass(in.GetRoutingClass()), TTLSeconds: in.GetTtlSeconds(), ValuesJSON: in.GetValuesJson(), RoutingKey: in.GetRoutingKey(), Enabled: in.GetEnabled()}
}

func serviceBackendRefToProto(in *metadata.ServiceBackendRef) *replpb.ServiceBackendRef {
	if in == nil {
		return nil
	}
	return &replpb.ServiceBackendRef{Id: in.ID, ServiceNamespace: in.ServiceNamespace, ServiceName: in.ServiceName, ServicePort: in.ServicePort}
}

func serviceBackendRefFromProto(in *replpb.ServiceBackendRef) *metadata.ServiceBackendRef {
	if in == nil {
		return nil
	}
	return &metadata.ServiceBackendRef{ID: in.GetId(), ServiceNamespace: in.GetServiceNamespace(), ServiceName: in.GetServiceName(), ServicePort: in.GetServicePort()}
}

func httpRouteProjectionToProto(in metadata.HTTPRouteProjection) *replpb.HTTPRouteProjection {
	return &replpb.HTTPRouteProjection{Id: in.ID, DomainEndpointId: in.DomainEndpointID, Hostname: in.Hostname, Path: in.Path, Priority: in.Priority, BackendRef: serviceBackendRefToProto(in.BackendRef)}
}

func httpRouteProjectionFromProto(in *replpb.HTTPRouteProjection) metadata.HTTPRouteProjection {
	if in == nil {
		return metadata.HTTPRouteProjection{}
	}
	return metadata.HTTPRouteProjection{ID: in.GetId(), DomainEndpointID: in.GetDomainEndpointId(), Hostname: in.GetHostname(), Path: in.GetPath(), Priority: in.GetPriority(), BackendRef: serviceBackendRefFromProto(in.GetBackendRef())}
}

func certificateRevisionToProto(in *metadata.CertificateRevision) *replpb.CertificateRevision {
	if in == nil {
		return nil
	}
	return &replpb.CertificateRevision{Id: in.ID, DomainEndpointId: in.DomainEndpointID, Hostname: in.Hostname, Revision: in.Revision, Provider: string(in.Provider), ChallengeType: string(in.ChallengeType), ArtifactBucket: in.ArtifactBucket, ArtifactPrefix: in.ArtifactPrefix, Sha256Crt: in.SHA256Crt, Sha256Key: in.SHA256Key, NotBefore: timeToProto(in.NotBefore), NotAfter: timeToProto(in.NotAfter)}
}

func certificateRevisionFromProto(in *replpb.CertificateRevision) *metadata.CertificateRevision {
	if in == nil {
		return nil
	}
	return &metadata.CertificateRevision{ID: in.GetId(), DomainEndpointID: in.GetDomainEndpointId(), Hostname: in.GetHostname(), Revision: in.GetRevision(), Provider: metadata.ACMEProvider(in.GetProvider()), ChallengeType: metadata.ChallengeType(in.GetChallengeType()), ArtifactBucket: in.GetArtifactBucket(), ArtifactPrefix: in.GetArtifactPrefix(), SHA256Crt: in.GetSha256Crt(), SHA256Key: in.GetSha256Key(), NotBefore: timeFromProto(in.GetNotBefore()), NotAfter: timeFromProto(in.GetNotAfter())}
}

func domainEntryProjectionToProto(in metadata.DomainEntryProjection) *replpb.DomainEntryProjection {
	out := &replpb.DomainEntryProjection{Id: in.ID, Hostname: in.Hostname, BackendType: string(in.BackendType), Cert: certificateRevisionToProto(in.Cert), BindedBackendRef: serviceBackendRefToProto(in.BindedBackendRef)}
	out.HttpRoutes = make([]*replpb.HTTPRouteProjection, 0, len(in.HTTPRoutes))
	for i := range in.HTTPRoutes {
		out.HttpRoutes = append(out.HttpRoutes, httpRouteProjectionToProto(in.HTTPRoutes[i]))
	}
	return out
}

func domainEntryProjectionFromProto(in *replpb.DomainEntryProjection) metadata.DomainEntryProjection {
	if in == nil {
		return metadata.DomainEntryProjection{}
	}
	out := metadata.DomainEntryProjection{ID: in.GetId(), Hostname: in.GetHostname(), BackendType: metadata.BackendType(in.GetBackendType()), Cert: certificateRevisionFromProto(in.GetCert()), BindedBackendRef: serviceBackendRefFromProto(in.GetBindedBackendRef())}
	out.HTTPRoutes = make([]metadata.HTTPRouteProjection, 0, len(in.GetHttpRoutes()))
	for _, item := range in.GetHttpRoutes() {
		out.HTTPRoutes = append(out.HTTPRoutes, httpRouteProjectionFromProto(item))
	}
	return out
}

func CollectSnapshotStream(stream SnapshotStream) ([]*Snapshot, error) {
	out := make([]*Snapshot, 0)
	for {
		item, err := stream.Recv()
		if err != nil {
			if err == io.EOF {
				return out, nil
			}
			return nil, err
		}
		out = append(out, item)
		if item != nil && item.Last {
			return out, nil
		}
	}
}

func timeToProto(t time.Time) *timestamp.Timestamp {
	if t.IsZero() {
		return nil
	}
	return &timestamp.Timestamp{Seconds: t.Unix(), Nanos: int32(t.Nanosecond())}
}

func timeFromProto(t *timestamp.Timestamp) time.Time {
	if t == nil {
		return time.Time{}
	}
	return time.Unix(t.Seconds, int64(t.Nanos)).UTC()
}
