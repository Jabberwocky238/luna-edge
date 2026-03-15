package engine

import (
	"context"
	"fmt"
	"time"

	"github.com/golang/protobuf/ptypes/timestamp"
	"github.com/jabberwocky238/luna-edge/replication/replpb"
	"github.com/jabberwocky238/luna-edge/repository/functions"
	"github.com/jabberwocky238/luna-edge/repository/metadata"
	"google.golang.org/grpc"
)

type RouteRecord struct {
	DomainID            string
	Hostname            string
	BindingID           string
	RouteVersion        uint64
	CertificateRevision uint64
	Listener            string
	Protocol            string
	UpstreamAddress     string
	UpstreamPort        uint32
	UpstreamProtocol    string
	BackendJSON         string
}

type BindingRecord struct {
	ID           string
	DomainID     string
	Hostname     string
	ServiceID    string
	Namespace    string
	Name         string
	Address      string
	Port         uint32
	Protocol     string
	RouteVersion uint64
	BackendJSON  string
}

type CertificateRecord struct {
	ID             string
	DomainID       string
	ZoneID         string
	Hostname       string
	Revision       uint64
	Status         string
	ArtifactBucket string
	ArtifactPrefix string
	SHA256Crt      string
	SHA256Key      string
	NotBefore      time.Time
	NotAfter       time.Time
}

type CertificateBundle struct {
	Hostname     string
	Revision     uint64
	TLSCrt       []byte
	TLSKey       []byte
	MetadataJSON []byte
}

type AssignmentRecord struct {
	ID                         string
	NodeID                     string
	DomainID                   string
	Hostname                   string
	Listener                   string
	BindingID                  string
	DesiredRouteVersion        uint64
	DesiredCertificateRevision uint64
	DesiredDNSVersion          uint64
	State                      string
	LastError                  string
}

type VersionVector struct {
	DesiredRouteVersion        uint64
	DesiredCertificateRevision uint64
	DesiredDNSVersion          uint64
}

func (v VersionVector) IsZero() bool {
	return v.DesiredRouteVersion == 0 && v.DesiredCertificateRevision == 0 && v.DesiredDNSVersion == 0
}

func (v VersionVector) DiffersFrom(other VersionVector) bool {
	return v.DesiredRouteVersion != other.DesiredRouteVersion ||
		v.DesiredCertificateRevision != other.DesiredCertificateRevision ||
		v.DesiredDNSVersion != other.DesiredDNSVersion
}

func (v VersionVector) IsNewerThan(other VersionVector) bool {
	return v.DesiredRouteVersion > other.DesiredRouteVersion ||
		v.DesiredCertificateRevision > other.DesiredCertificateRevision ||
		v.DesiredDNSVersion > other.DesiredDNSVersion
}

type Snapshot struct {
	NodeID       string
	CreatedAt    time.Time
	Versions     VersionVector
	Routes       []RouteRecord
	Bindings     []BindingRecord
	Certificates []CertificateRecord
	Assignments  []AssignmentRecord
}

type ChangeNotification struct {
	NodeID    string
	Versions  VersionVector
	CreatedAt time.Time
}

type SnapshotStore interface {
	BuildSnapshot(ctx context.Context, nodeID string) (*Snapshot, error)
}

type Publisher interface {
	PublishSnapshot(ctx context.Context, snapshot *Snapshot) error
	PublishNode(ctx context.Context, nodeID string) error
}

type NoticeStream interface {
	Recv() (*ChangeNotification, error)
}

type Client interface {
	GetSnapshot(ctx context.Context, nodeID string) (*Snapshot, error)
	Subscribe(ctx context.Context, nodeID string, known VersionVector) (NoticeStream, error)
	FetchCertificateBundle(ctx context.Context, hostname string, revision uint64) (*CertificateBundle, error)
}

type Subscriber interface {
	Subscribe(ctx context.Context, nodeID string, known VersionVector) error
}

type SnapshotApplier interface {
	ApplySnapshot(ctx context.Context, snapshot *Snapshot) error
}

type ProjectionBuilder interface {
	BuildRouteRecord(ctx context.Context, domainID string) (*RouteRecord, error)
	BuildBindingRecord(ctx context.Context, domainID string) (*BindingRecord, error)
	BuildCertificateRecord(ctx context.Context, domainID string, revision uint64) (*CertificateRecord, error)
	BuildAssignmentRecord(ctx context.Context, attachment *metadata.Attachment) (*AssignmentRecord, error)
}

type GRPCClient struct {
	client replpb.ReplicationServiceClient
}

func NewGRPCClient(conn grpc.ClientConnInterface) *GRPCClient {
	return &GRPCClient{client: replpb.NewReplicationServiceClient(conn)}
}

func (c *GRPCClient) GetSnapshot(ctx context.Context, nodeID string) (*Snapshot, error) {
	resp, err := c.client.GetSnapshot(ctx, &replpb.SnapshotRequest{NodeId: nodeID})
	if err != nil {
		return nil, err
	}
	return SnapshotFromProto(resp), nil
}

func (c *GRPCClient) Subscribe(ctx context.Context, nodeID string, known VersionVector) (NoticeStream, error) {
	stream, err := c.client.Subscribe(ctx, &replpb.SubscriptionRequest{
		NodeId:        nodeID,
		KnownVersions: VersionVectorToProto(known),
	})
	if err != nil {
		return nil, err
	}
	return grpcNoticeStream{stream: stream}, nil
}

func (c *GRPCClient) FetchCertificateBundle(ctx context.Context, hostname string, revision uint64) (*CertificateBundle, error) {
	resp, err := c.client.FetchCertificateBundle(ctx, &replpb.CertificateBundleRequest{
		Hostname: hostname,
		Revision: revision,
	})
	if err != nil {
		return nil, err
	}
	if resp == nil {
		return nil, fmt.Errorf("certificate bundle response is nil")
	}
	return &CertificateBundle{
		Hostname:     resp.GetHostname(),
		Revision:     resp.GetRevision(),
		TLSCrt:       append([]byte(nil), resp.GetTlsCrt()...),
		TLSKey:       append([]byte(nil), resp.GetTlsKey()...),
		MetadataJSON: append([]byte(nil), resp.GetMetadataJson()...),
	}, nil
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

type RepositoryProjectionBuilder struct {
	Repo functions.Repository
}

func NewRepositoryProjectionBuilder(repo functions.Repository) (*RepositoryProjectionBuilder, error) {
	if repo == nil {
		return nil, fmt.Errorf("repository is required")
	}
	return &RepositoryProjectionBuilder{Repo: repo}, nil
}

func (b *RepositoryProjectionBuilder) BuildRouteRecord(ctx context.Context, domainID string) (*RouteRecord, error) {
	if domainID == "" {
		return nil, fmt.Errorf("domain id is required")
	}
	route := &metadata.RouteProjection{}
	if err := b.Repo.RouteProjections().GetResourceByField(ctx, route, "domain_id", domainID); err != nil {
		return nil, err
	}
	binding, err := b.Repo.GetServiceBindingByDomainID(ctx, domainID)
	if err != nil {
		return nil, err
	}
	record := &RouteRecord{
		DomainID:         domainID,
		Hostname:         route.Hostname,
		BindingID:        route.BindingID,
		RouteVersion:     route.RouteVersion,
		Protocol:         route.Protocol,
		UpstreamAddress:  binding.Address,
		UpstreamPort:     binding.Port,
		UpstreamProtocol: binding.Protocol,
		BackendJSON:      binding.BackendJSON,
	}
	status, err := b.Repo.GetDomainEndpointStatus(ctx, domainID)
	if err == nil && status != nil {
		record.CertificateRevision = status.CertificateRevision
	}
	attachments, err := b.Repo.ListAttachmentsByDomainID(ctx, domainID)
	if err == nil && len(attachments) > 0 {
		record.Listener = attachments[0].Listener
		if record.CertificateRevision == 0 {
			record.CertificateRevision = attachments[0].DesiredCertificateRevision
		}
	}
	return record, nil
}

func (b *RepositoryProjectionBuilder) BuildBindingRecord(ctx context.Context, domainID string) (*BindingRecord, error) {
	if domainID == "" {
		return nil, fmt.Errorf("domain id is required")
	}
	binding, err := b.Repo.GetServiceBindingByDomainID(ctx, domainID)
	if err != nil {
		return nil, err
	}
	return &BindingRecord{
		ID:           binding.ID,
		DomainID:     binding.DomainID,
		Hostname:     binding.Hostname,
		ServiceID:    binding.ServiceID,
		Namespace:    binding.Namespace,
		Name:         binding.Name,
		Address:      binding.Address,
		Port:         binding.Port,
		Protocol:     binding.Protocol,
		RouteVersion: binding.RouteVersion,
		BackendJSON:  binding.BackendJSON,
	}, nil
}

func (b *RepositoryProjectionBuilder) BuildCertificateRecord(ctx context.Context, domainID string, revision uint64) (*CertificateRecord, error) {
	if domainID == "" {
		return nil, fmt.Errorf("domain id is required")
	}
	var (
		cert *metadata.CertificateRevision
		err  error
	)
	if revision > 0 {
		cert, err = b.Repo.GetCertificateRevision(ctx, domainID, revision)
	} else {
		cert, err = b.Repo.GetLatestCertificateRevision(ctx, domainID)
	}
	if err != nil {
		return nil, err
	}
	if cert == nil {
		return nil, fmt.Errorf("certificate revision not found")
	}
	return &CertificateRecord{
		ID:             cert.ID,
		DomainID:       cert.DomainID,
		ZoneID:         cert.ZoneID,
		Hostname:       cert.Hostname,
		Revision:       cert.Revision,
		Status:         string(cert.Status),
		ArtifactBucket: cert.ArtifactBucket,
		ArtifactPrefix: cert.ArtifactPrefix,
		SHA256Crt:      cert.SHA256Crt,
		SHA256Key:      cert.SHA256Key,
		NotBefore:      cert.NotBefore,
		NotAfter:       cert.NotAfter,
	}, nil
}

func (b *RepositoryProjectionBuilder) BuildAssignmentRecord(ctx context.Context, attachment *metadata.Attachment) (*AssignmentRecord, error) {
	if attachment == nil {
		return nil, fmt.Errorf("attachment is required")
	}
	binding, err := b.Repo.GetServiceBindingByDomainID(ctx, attachment.DomainID)
	if err != nil {
		return nil, err
	}
	domain := &metadata.DomainEndpoint{}
	if err := b.Repo.DomainEndpoints().GetResourceByField(ctx, domain, "id", attachment.DomainID); err != nil {
		return nil, err
	}
	return &AssignmentRecord{
		ID:                         attachment.ID,
		NodeID:                     attachment.NodeID,
		DomainID:                   attachment.DomainID,
		Hostname:                   domain.Hostname,
		Listener:                   attachment.Listener,
		BindingID:                  binding.ID,
		DesiredRouteVersion:        attachment.DesiredRouteVersion,
		DesiredCertificateRevision: attachment.DesiredCertificateRevision,
		DesiredDNSVersion:          attachment.DesiredDNSVersion,
		State:                      attachment.State,
		LastError:                  attachment.LastError,
	}, nil
}

func SnapshotToProto(in *Snapshot) *replpb.Snapshot {
	if in == nil {
		return nil
	}
	out := &replpb.Snapshot{
		NodeId:    in.NodeID,
		CreatedAt: timeToProto(in.CreatedAt),
		Versions:  VersionVectorToProto(in.Versions),
	}
	out.Routes = make([]*replpb.RouteRecord, 0, len(in.Routes))
	for i := range in.Routes {
		out.Routes = append(out.Routes, routeToProto(in.Routes[i]))
	}
	out.Bindings = make([]*replpb.BindingRecord, 0, len(in.Bindings))
	for i := range in.Bindings {
		out.Bindings = append(out.Bindings, bindingToProto(in.Bindings[i]))
	}
	out.Certificates = make([]*replpb.CertificateRecord, 0, len(in.Certificates))
	for i := range in.Certificates {
		out.Certificates = append(out.Certificates, certificateToProto(in.Certificates[i]))
	}
	out.Assignments = make([]*replpb.AssignmentRecord, 0, len(in.Assignments))
	for i := range in.Assignments {
		out.Assignments = append(out.Assignments, assignmentToProto(in.Assignments[i]))
	}
	return out
}

func SnapshotFromProto(in *replpb.Snapshot) *Snapshot {
	if in == nil {
		return nil
	}
	out := &Snapshot{
		NodeID:    in.GetNodeId(),
		CreatedAt: timeFromProto(in.GetCreatedAt()),
		Versions:  VersionVectorFromProto(in.GetVersions()),
	}
	out.Routes = make([]RouteRecord, 0, len(in.GetRoutes()))
	for _, route := range in.GetRoutes() {
		out.Routes = append(out.Routes, routeFromProto(route))
	}
	out.Bindings = make([]BindingRecord, 0, len(in.GetBindings()))
	for _, binding := range in.GetBindings() {
		out.Bindings = append(out.Bindings, bindingFromProto(binding))
	}
	out.Certificates = make([]CertificateRecord, 0, len(in.GetCertificates()))
	for _, cert := range in.GetCertificates() {
		out.Certificates = append(out.Certificates, certificateFromProto(cert))
	}
	out.Assignments = make([]AssignmentRecord, 0, len(in.GetAssignments()))
	for _, assignment := range in.GetAssignments() {
		out.Assignments = append(out.Assignments, assignmentFromProto(assignment))
	}
	return out
}

func ChangeNotificationToProto(in *ChangeNotification) *replpb.ChangeNotification {
	if in == nil {
		return nil
	}
	return &replpb.ChangeNotification{
		NodeId:    in.NodeID,
		Versions:  VersionVectorToProto(in.Versions),
		CreatedAt: timeToProto(in.CreatedAt),
	}
}

func ChangeNotificationFromProto(in *replpb.ChangeNotification) *ChangeNotification {
	if in == nil {
		return nil
	}
	return &ChangeNotification{
		NodeID:    in.GetNodeId(),
		Versions:  VersionVectorFromProto(in.GetVersions()),
		CreatedAt: timeFromProto(in.GetCreatedAt()),
	}
}

func VersionVectorToProto(in VersionVector) *replpb.VersionVector {
	return &replpb.VersionVector{
		DesiredRouteVersion:        in.DesiredRouteVersion,
		DesiredCertificateRevision: in.DesiredCertificateRevision,
		DesiredDnsVersion:          in.DesiredDNSVersion,
	}
}

func VersionVectorFromProto(in *replpb.VersionVector) VersionVector {
	if in == nil {
		return VersionVector{}
	}
	return VersionVector{
		DesiredRouteVersion:        in.GetDesiredRouteVersion(),
		DesiredCertificateRevision: in.GetDesiredCertificateRevision(),
		DesiredDNSVersion:          in.GetDesiredDnsVersion(),
	}
}

func routeToProto(in RouteRecord) *replpb.RouteRecord {
	return &replpb.RouteRecord{
		DomainId:            in.DomainID,
		Hostname:            in.Hostname,
		BindingId:           in.BindingID,
		RouteVersion:        in.RouteVersion,
		CertificateRevision: in.CertificateRevision,
		Listener:            in.Listener,
		Protocol:            in.Protocol,
		UpstreamAddress:     in.UpstreamAddress,
		UpstreamPort:        in.UpstreamPort,
		UpstreamProtocol:    in.UpstreamProtocol,
		BackendJson:         in.BackendJSON,
	}
}

func routeFromProto(in *replpb.RouteRecord) RouteRecord {
	return RouteRecord{
		DomainID:            in.GetDomainId(),
		Hostname:            in.GetHostname(),
		BindingID:           in.GetBindingId(),
		RouteVersion:        in.GetRouteVersion(),
		CertificateRevision: in.GetCertificateRevision(),
		Listener:            in.GetListener(),
		Protocol:            in.GetProtocol(),
		UpstreamAddress:     in.GetUpstreamAddress(),
		UpstreamPort:        in.GetUpstreamPort(),
		UpstreamProtocol:    in.GetUpstreamProtocol(),
		BackendJSON:         in.GetBackendJson(),
	}
}

func bindingToProto(in BindingRecord) *replpb.BindingRecord {
	return &replpb.BindingRecord{
		Id:           in.ID,
		DomainId:     in.DomainID,
		Hostname:     in.Hostname,
		ServiceId:    in.ServiceID,
		Namespace:    in.Namespace,
		Name:         in.Name,
		Address:      in.Address,
		Port:         in.Port,
		Protocol:     in.Protocol,
		RouteVersion: in.RouteVersion,
		BackendJson:  in.BackendJSON,
	}
}

func bindingFromProto(in *replpb.BindingRecord) BindingRecord {
	return BindingRecord{
		ID:           in.GetId(),
		DomainID:     in.GetDomainId(),
		Hostname:     in.GetHostname(),
		ServiceID:    in.GetServiceId(),
		Namespace:    in.GetNamespace(),
		Name:         in.GetName(),
		Address:      in.GetAddress(),
		Port:         in.GetPort(),
		Protocol:     in.GetProtocol(),
		RouteVersion: in.GetRouteVersion(),
		BackendJSON:  in.GetBackendJson(),
	}
}

func certificateToProto(in CertificateRecord) *replpb.CertificateRecord {
	return &replpb.CertificateRecord{
		Id:             in.ID,
		DomainId:       in.DomainID,
		ZoneId:         in.ZoneID,
		Hostname:       in.Hostname,
		Revision:       in.Revision,
		Status:         in.Status,
		ArtifactBucket: in.ArtifactBucket,
		ArtifactPrefix: in.ArtifactPrefix,
		Sha256Crt:      in.SHA256Crt,
		Sha256Key:      in.SHA256Key,
		NotBefore:      timeToProto(in.NotBefore),
		NotAfter:       timeToProto(in.NotAfter),
	}
}

func certificateFromProto(in *replpb.CertificateRecord) CertificateRecord {
	return CertificateRecord{
		ID:             in.GetId(),
		DomainID:       in.GetDomainId(),
		ZoneID:         in.GetZoneId(),
		Hostname:       in.GetHostname(),
		Revision:       in.GetRevision(),
		Status:         in.GetStatus(),
		ArtifactBucket: in.GetArtifactBucket(),
		ArtifactPrefix: in.GetArtifactPrefix(),
		SHA256Crt:      in.GetSha256Crt(),
		SHA256Key:      in.GetSha256Key(),
		NotBefore:      timeFromProto(in.GetNotBefore()),
		NotAfter:       timeFromProto(in.GetNotAfter()),
	}
}

func assignmentToProto(in AssignmentRecord) *replpb.AssignmentRecord {
	return &replpb.AssignmentRecord{
		Id:                         in.ID,
		NodeId:                     in.NodeID,
		DomainId:                   in.DomainID,
		Hostname:                   in.Hostname,
		Listener:                   in.Listener,
		BindingId:                  in.BindingID,
		DesiredRouteVersion:        in.DesiredRouteVersion,
		DesiredCertificateRevision: in.DesiredCertificateRevision,
		DesiredDnsVersion:          in.DesiredDNSVersion,
		State:                      in.State,
		LastError:                  in.LastError,
	}
}

func assignmentFromProto(in *replpb.AssignmentRecord) AssignmentRecord {
	return AssignmentRecord{
		ID:                         in.GetId(),
		NodeID:                     in.GetNodeId(),
		DomainID:                   in.GetDomainId(),
		Hostname:                   in.GetHostname(),
		Listener:                   in.GetListener(),
		BindingID:                  in.GetBindingId(),
		DesiredRouteVersion:        in.GetDesiredRouteVersion(),
		DesiredCertificateRevision: in.GetDesiredCertificateRevision(),
		DesiredDNSVersion:          in.GetDesiredDnsVersion(),
		State:                      in.GetState(),
		LastError:                  in.GetLastError(),
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
