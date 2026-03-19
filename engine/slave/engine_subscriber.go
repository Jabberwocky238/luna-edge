package slave

import (
	"context"
	"errors"
	"io"
	"log"

	"github.com/jabberwocky238/luna-edge/replication"
)

func (s *Engine) FetchCertificateBundle(ctx context.Context, hostname string, revision uint64) (*replication.CertificateBundle, error) {
	if s == nil || s.grpcClient == nil {
		return nil, nil
	}
	return s.grpcClient.FetchCertificateBundle(ctx, hostname, revision)
}

// CatchUpSnapshots 从 master 获取快照数据并应用到本地，直到快照记录 ID 超过指定的 snapshotRecordID。
func (s *Engine) CatchUpSnapshots(ctx context.Context, nodeID string, snapshotRecordID uint64) error {
	log.Printf("slave: catch-up begin node_id=%s cursor=%d", nodeID, snapshotRecordID)
	snapshotStream, err := s.grpcClient.GetSnapshot(ctx, nodeID, snapshotRecordID)
	if err != nil {
		log.Printf("slave: catch-up open snapshot stream failed node_id=%s cursor=%d err=%v", nodeID, snapshotRecordID, err)
		return err
	}
	for {
		snapshot, recvErr := snapshotStream.Recv()
		if recvErr != nil {
			log.Printf("slave: catch-up recv failed node_id=%s cursor=%d err=%v", nodeID, snapshotRecordID, recvErr)
			return recvErr
		}
		if snapshot == nil {
			continue
		}
		log.Printf("slave: apply snapshot begin node_id=%s snapshot_record_id=%d last=%v dns=%d domains=%d", nodeID, snapshot.SnapshotRecordID, snapshot.Last, len(snapshot.DNSRecords), len(snapshot.DomainEntries))
		if err := s.Cache.ApplySnapshot(ctx, snapshot); err != nil {
			log.Printf("slave: apply snapshot failed node_id=%s snapshot_record_id=%d err=%v", nodeID, snapshot.SnapshotRecordID, err)
			return err
		}
		log.Printf("slave: apply snapshot done node_id=%s snapshot_record_id=%d", nodeID, snapshot.SnapshotRecordID)
		if snapshot.Last {
			log.Printf("slave: catch-up done node_id=%s snapshot_record_id=%d", nodeID, snapshot.SnapshotRecordID)
			return nil
		}
	}
}

// Subscribe 订阅 master 的变更通知，并应用到本地。
func (s *Engine) Subscribe(ctx context.Context, nodeID string) error {
	if s.grpcClient == nil {
		return errors.New("s.grpcClient == nil")
	}
	stream, err := s.grpcClient.Subscribe(ctx, nodeID)
	if err != nil {
		log.Printf("slave: open notice stream failed node_id=%s err=%v", nodeID, err)
		return err
	}
	log.Printf("slave: notice stream opened node_id=%s", nodeID)
	cursor, err := s.Cache.GetSnapshotRecordID(ctx)
	if err != nil {
		return err
	}
	for {
		notice, recvErr := stream.Recv()
		if recvErr != nil {
			if recvErr == io.EOF {
				log.Printf("slave: notice stream closed by server node_id=%s", nodeID)
				return nil
			}
			log.Printf("slave: notice recv failed node_id=%s err=%v", nodeID, recvErr)
			return recvErr
		}
		if notice == nil {
			continue
		}
		log.Printf("slave: notice received node_id=%s snapshot_record_id=%d dns=%v domain=%v", nodeID, notice.SnapshotRecordID, notice.DNSRecord != nil, notice.DomainEntry != nil)
		if notice.SnapshotRecordID <= cursor {
			continue
		}
		if notice.SnapshotRecordID > cursor+1 {
			// missed some notices, need to catch up snapshots before applying the new notice
			if err := s.CatchUpSnapshots(ctx, nodeID, cursor); err != nil {
				log.Printf("slave: catch-up after notice failed node_id=%s snapshot_record_id=%d err=%v", nodeID, notice.SnapshotRecordID, err)
				return err
			}
			// update cursor
			newcursor, err := s.Cache.GetSnapshotRecordID(ctx)
			if err != nil {
				return err
			}
			cursor = newcursor
			continue
		}
		log.Printf("slave: apply notice begin node_id=%s snapshot_record_id=%d", nodeID, notice.SnapshotRecordID)
		if err := s.Cache.ApplyChangelog(ctx, notice); err != nil {
			log.Printf("slave: apply notice failed node_id=%s snapshot_record_id=%d err=%v", nodeID, notice.SnapshotRecordID, err)
			return err
		}
		log.Printf("slave: apply notice done node_id=%s snapshot_record_id=%d", nodeID, notice.SnapshotRecordID)
		cursor = notice.SnapshotRecordID
	}
}
