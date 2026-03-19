package master

import (
	"context"
	"errors"
	"log"
	"sync"
	"time"

	"github.com/jabberwocky238/luna-edge/replication"
	"github.com/jabberwocky238/luna-edge/repository/metadata"
	"gorm.io/gorm"
)

// Hub 管理所有 slave 订阅流并负责 fan-out。
type Hub struct {
	mu sync.RWMutex
	// map[nodeID]map[subscriptionID]chan*replication.ChangeNotification
	subscribers map[string]map[uint64]chan *replication.ChangeNotification
	nextID      uint64
}

func NewHub() *Hub {
	return &Hub{subscribers: make(map[string]map[uint64]chan *replication.ChangeNotification)}
}

func (h *Hub) Subscribe(nodeID string, buffer int) (uint64, <-chan *replication.ChangeNotification) {
	h.mu.Lock()
	defer h.mu.Unlock()
	h.nextID++
	id := h.nextID
	if h.subscribers[nodeID] == nil {
		h.subscribers[nodeID] = make(map[uint64]chan *replication.ChangeNotification)
	}
	ch := make(chan *replication.ChangeNotification, buffer)
	h.subscribers[nodeID][id] = ch
	return id, ch
}

func (h *Hub) Unsubscribe(nodeID string, id uint64) {
	h.mu.Lock()
	defer h.mu.Unlock()
	nodeSubs := h.subscribers[nodeID]
	if nodeSubs == nil {
		return
	}
	ch, ok := nodeSubs[id]
	if !ok {
		return
	}
	delete(nodeSubs, id)
	close(ch)
	if len(nodeSubs) == 0 {
		delete(h.subscribers, nodeID)
	}
}

func (h *Hub) Publish(nodeID string, notification *replication.ChangeNotification) {
	h.mu.RLock()
	defer h.mu.RUnlock()
	nodeSubs := h.subscribers[nodeID]
	for _, ch := range nodeSubs {
		select {
		case ch <- notification:
		default:
			// 如果缓冲区满了，就丢弃这个通知，避免阻塞。
		}
	}
}

func (h *Hub) Boardcast(notification *replication.ChangeNotification) {
	h.mu.RLock()
	defer h.mu.RUnlock()
	for _, nodeSubs := range h.subscribers {
		for _, ch := range nodeSubs {
			select {
			case ch <- notification:
			default:
				// 如果缓冲区满了，就丢弃这个通知，避免阻塞。
			}
		}
	}
}

func (e *Engine) appendSnapshotRecord(ctx context.Context, syncType metadata.SnapshotSyncType, syncID string, action metadata.SnapshotAction) (uint64, error) {
	record := &metadata.SnapshotRecord{SyncType: syncType, SyncID: syncID, Action: action}
	if err := e.Repo.AppendSnapshotRecord(ctx, record); err != nil {
		log.Printf("replication: append snapshot record failed type=%s sync_id=%s action=%s err=%v", syncType, syncID, action, err)
		return 0, err
	}
	log.Printf("replication: append snapshot record done id=%d type=%s sync_id=%s action=%s", record.ID, syncType, syncID, action)
	return record.ID, nil
}

func (e *Engine) BoardcastDNSRecord(ctx context.Context, recordID string) error {
	record := &metadata.DNSRecord{}
	if err := e.Repo.DNSRecords().GetResourceByField(ctx, record, "id", recordID); err != nil {
		if errors.Is(err, gorm.ErrRecordNotFound) {
			return nil
		}
		return err
	}
	snapshotAction := func() metadata.SnapshotAction {
		if record.Deleted {
			return metadata.SnapshotActionDelete
		} else {
			return metadata.SnapshotActionUpsert
		}
	}()
	snapshotRecordID, err := e.appendSnapshotRecord(ctx, metadata.SnapshotSyncTypeDNSRecord, recordID, snapshotAction)
	if err != nil {
		return err
	}
	e.Hub.Boardcast(&replication.ChangeNotification{
		NodeID:           e.NODE_ID,
		CreatedAt:        time.Now().UTC(),
		SnapshotRecordID: snapshotRecordID,
		DNSRecord:        record,
	})
	return nil
}

func (e *Engine) BoardcastDomainEndpointProjection(ctx context.Context, hostname string) error {
	entry, err := e.Repo.GetDomainEntryProjectionByDomain(ctx, hostname)
	if err != nil {
		if errors.Is(err, gorm.ErrRecordNotFound) {
			return nil
		}
		return err
	}
	if entry == nil {
		return errors.New("BoardcastDomainEndpointProjection: entry is nil")
	}
	snapshotAction := func() metadata.SnapshotAction {
		if entry.Deleted {
			return metadata.SnapshotActionDelete
		} else {
			return metadata.SnapshotActionUpsert
		}
	}()
	snapshotRecordID, err := e.appendSnapshotRecord(ctx, metadata.SnapshotSyncTypeDomainEntryProjection, hostname, snapshotAction)
	if err != nil {
		return err
	}
	e.Hub.Boardcast(&replication.ChangeNotification{
		NodeID:           e.NODE_ID,
		CreatedAt:        time.Now().UTC(),
		SnapshotRecordID: snapshotRecordID,
		DomainEntry:      entry,
	})
	return nil
}
