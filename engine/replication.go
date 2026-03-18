package engine

import (
	"context"
	"time"

	"github.com/jabberwocky238/luna-edge/repository/metadata"
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
	DNSRecords       []metadata.DNSRecord
	DomainEntries    []metadata.DomainEntryProjection
	Last             bool
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

type Publisher interface {
	PublishChangeLog(ctx context.Context, changelog *ChangeNotification) error
}
