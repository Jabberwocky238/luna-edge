package slave

import (
	"context"
	"fmt"
	"io"
	"log"
)

type streamSubscriber struct {
	Client       *LocalStore
	OnSnapshot   func(context.Context, *engine.Snapshot) error
	OnConnect    func()
	OnDisconnect func()
}

func (s *streamSubscriber) Subscribe(ctx context.Context, nodeID string) error {
	if s.Client == nil {
		return fmt.Errorf("replication client is not configured")
	}
	if s.Applier == nil {
		return fmt.Errorf("replication applier is not configured")
	}
	stream, err := s.Client.Subscribe(ctx, nodeID)
	if err != nil {
		log.Printf("slave: open notice stream failed node_id=%s err=%v", nodeID, err)
		return err
	}
	log.Printf("slave: notice stream opened node_id=%s", nodeID)
	if s.OnConnect != nil {
		s.OnConnect()
	}
	defer func() {
		if s.OnDisconnect != nil {
			s.OnDisconnect()
		}
	}()
	var cursor uint64
	if provider, ok := s.Applier.(interface {
		GetSnapshotRecordID(context.Context) (uint64, error)
	}); ok {
		value, err := provider.GetSnapshotRecordID(ctx)
		if err != nil {
			return err
		}
		cursor = value
	}
	log.Printf("slave: local snapshot cursor node_id=%s cursor=%d", nodeID, cursor)
	if err := s.catchUpSnapshots(ctx, nodeID, cursor); err != nil {
		log.Printf("slave: initial catch-up failed node_id=%s cursor=%d err=%v", nodeID, cursor, err)
		return err
	}
	log.Printf("slave: initial catch-up done node_id=%s cursor=%d", nodeID, cursor)
	if provider, ok := s.Applier.(interface {
		GetSnapshotRecordID(context.Context) (uint64, error)
	}); ok {
		value, err := provider.GetSnapshotRecordID(ctx)
		if err != nil {
			return err
		}
		cursor = value
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
			if err := s.catchUpSnapshots(ctx, nodeID, cursor); err != nil {
				log.Printf("slave: catch-up after notice failed node_id=%s snapshot_record_id=%d err=%v", nodeID, notice.SnapshotRecordID, err)
				return err
			}
			if provider, ok := s.Applier.(interface {
				GetSnapshotRecordID(context.Context) (uint64, error)
			}); ok {
				value, err := provider.GetSnapshotRecordID(ctx)
				if err != nil {
					return err
				}
				cursor = value
			}
			continue
		}
		snapshot := snapshotFromNotice(notice)
		log.Printf("slave: apply notice snapshot begin node_id=%s snapshot_record_id=%d dns=%d domains=%d", nodeID, snapshot.SnapshotRecordID, len(snapshot.DNSRecords), len(snapshot.DomainEntries))
		if err := s.Applier.ApplySnapshot(ctx, snapshot); err != nil {
			log.Printf("slave: apply notice snapshot failed node_id=%s snapshot_record_id=%d err=%v", nodeID, snapshot.SnapshotRecordID, err)
			return err
		}
		log.Printf("slave: apply notice snapshot done node_id=%s snapshot_record_id=%d", nodeID, snapshot.SnapshotRecordID)
		if s.OnSnapshot != nil {
			if err := s.OnSnapshot(ctx, snapshot); err != nil {
				log.Printf("slave: on notice snapshot hook failed node_id=%s snapshot_record_id=%d err=%v", nodeID, snapshot.SnapshotRecordID, err)
				return err
			}
			log.Printf("slave: on notice snapshot hook done node_id=%s snapshot_record_id=%d", nodeID, snapshot.SnapshotRecordID)
		}
		cursor = notice.SnapshotRecordID
	}
}

func snapshotFromNotice(notice *engine.ChangeNotification) *engine.Snapshot {
	snapshot := &engine.Snapshot{
		NodeID:           notice.NodeID,
		CreatedAt:        notice.CreatedAt,
		SnapshotRecordID: notice.SnapshotRecordID,
		Last:             true,
	}
	if notice.DNSRecord != nil {
		snapshot.DNSRecords = append(snapshot.DNSRecords, *notice.DNSRecord)
	}
	if notice.DomainEntry != nil {
		snapshot.DomainEntries = append(snapshot.DomainEntries, *notice.DomainEntry)
	}
	return snapshot
}

func (s *streamSubscriber) catchUpSnapshots(ctx context.Context, nodeID string, cursor uint64) error {
	log.Printf("slave: catch-up begin node_id=%s cursor=%d", nodeID, cursor)
	snapshotStream, err := s.Client.GetSnapshot(ctx, nodeID, cursor)
	if err != nil {
		log.Printf("slave: catch-up open snapshot stream failed node_id=%s cursor=%d err=%v", nodeID, cursor, err)
		return err
	}
	for {
		snapshot, recvErr := snapshotStream.Recv()
		if recvErr != nil {
			if recvErr == io.EOF {
				log.Printf("slave: catch-up stream eof node_id=%s cursor=%d", nodeID, cursor)
				return nil
			}
			log.Printf("slave: catch-up recv failed node_id=%s cursor=%d err=%v", nodeID, cursor, recvErr)
			return recvErr
		}
		if snapshot == nil {
			continue
		}
		log.Printf("slave: apply snapshot begin node_id=%s snapshot_record_id=%d last=%v dns=%d domains=%d", nodeID, snapshot.SnapshotRecordID, snapshot.Last, len(snapshot.DNSRecords), len(snapshot.DomainEntries))
		if err := s.Applier.ApplySnapshot(ctx, snapshot); err != nil {
			log.Printf("slave: apply snapshot failed node_id=%s snapshot_record_id=%d err=%v", nodeID, snapshot.SnapshotRecordID, err)
			return err
		}
		log.Printf("slave: apply snapshot done node_id=%s snapshot_record_id=%d", nodeID, snapshot.SnapshotRecordID)
		if s.OnSnapshot != nil {
			if err := s.OnSnapshot(ctx, snapshot); err != nil {
				log.Printf("slave: on snapshot hook failed node_id=%s snapshot_record_id=%d err=%v", nodeID, snapshot.SnapshotRecordID, err)
				return err
			}
			log.Printf("slave: on snapshot hook done node_id=%s snapshot_record_id=%d", nodeID, snapshot.SnapshotRecordID)
		}
		if snapshot.Last {
			log.Printf("slave: catch-up done node_id=%s snapshot_record_id=%d", nodeID, snapshot.SnapshotRecordID)
			return nil
		}
	}
}
