package replication

import (
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
