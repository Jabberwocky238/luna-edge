package master

import (
	"sync"

	enginepkg "github.com/jabberwocky238/luna-edge/engine"
)

// Hub 管理所有 slave 订阅流并负责 fan-out。
type Hub struct {
	mu          sync.RWMutex
	subscribers map[string]map[uint64]chan *enginepkg.ChangeNotification
	nextID      uint64
}

func NewHub() *Hub {
	return &Hub{subscribers: make(map[string]map[uint64]chan *enginepkg.ChangeNotification)}
}

func (h *Hub) Subscribe(nodeID string, buffer int) (uint64, <-chan *enginepkg.ChangeNotification) {
	h.mu.Lock()
	defer h.mu.Unlock()
	h.nextID++
	id := h.nextID
	if h.subscribers[nodeID] == nil {
		h.subscribers[nodeID] = make(map[uint64]chan *enginepkg.ChangeNotification)
	}
	ch := make(chan *enginepkg.ChangeNotification, buffer)
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

func (h *Hub) Publish(nodeID string, msg *enginepkg.ChangeNotification) {
	h.mu.RLock()
	nodeSubs := h.subscribers[nodeID]
	targets := make([]chan *enginepkg.ChangeNotification, 0, len(nodeSubs))
	for _, ch := range nodeSubs {
		targets = append(targets, ch)
	}
	h.mu.RUnlock()
	for _, ch := range targets {
		ch <- msg
	}
}

func (h *Hub) PublishAll(msg *enginepkg.ChangeNotification) {
	h.mu.RLock()
	targets := make([]chan *enginepkg.ChangeNotification, 0)
	for _, nodeSubs := range h.subscribers {
		for _, ch := range nodeSubs {
			targets = append(targets, ch)
		}
	}
	h.mu.RUnlock()
	for _, ch := range targets {
		ch <- msg
	}
}
