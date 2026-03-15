package ingress

import (
	"context"
	"fmt"
	"sync"

	enginepkg "github.com/jabberwocky238/luna-edge/engine"
	"github.com/jabberwocky238/luna-edge/repository/metadata"
)

type memoryStore struct {
	mu       sync.RWMutex
	bindings map[string]*metadata.ServiceBinding
}

func newMemoryStore() *memoryStore {
	return &memoryStore{
		bindings: map[string]*metadata.ServiceBinding{},
	}
}

func (s *memoryStore) Get(hostname string) (*metadata.ServiceBinding, bool) {
	s.mu.RLock()
	defer s.mu.RUnlock()
	binding, ok := s.bindings[normalizeHost(hostname)]
	if !ok || binding == nil {
		return nil, false
	}
	copyBinding := *binding
	return &copyBinding, true
}

func (s *memoryStore) Put(binding *metadata.ServiceBinding) {
	if binding == nil {
		return
	}
	s.mu.Lock()
	defer s.mu.Unlock()
	copyBinding := *binding
	s.bindings[normalizeHost(binding.Hostname)] = &copyBinding
}

func (s *memoryStore) Delete(hostname string) {
	s.mu.Lock()
	defer s.mu.Unlock()
	delete(s.bindings, normalizeHost(hostname))
}

func (s *memoryStore) Clear() {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.bindings = map[string]*metadata.ServiceBinding{}
}

type BindingLookupReader interface {
	GetBindingByHostname(ctx context.Context, hostname string) (*enginepkg.BindingRecord, error)
}

type ReplicaReader interface {
	ReadCache() BindingLookupReader
}

func lookupBindingFromReadOnlyCache(ctx context.Context, slave ReplicaReader, hostname string) (*enginepkg.BindingRecord, error) {
	if slave == nil {
		return nil, fmt.Errorf("slave is nil")
	}
	cache := slave.ReadCache()
	if cache == nil {
		return nil, fmt.Errorf("slave cache is nil")
	}
	return cache.GetBindingByHostname(ctx, hostname)
}
