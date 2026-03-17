package ingress

import (
	"context"
	"fmt"
	"sync"

	"github.com/jabberwocky238/luna-edge/repository/metadata"
)

type memoryStore struct {
	mu       sync.RWMutex
	bindings map[string][]*BackendBinding
}

func newMemoryStore() *memoryStore {
	return &memoryStore{
		bindings: map[string][]*BackendBinding{},
	}
}

func (s *memoryStore) Get(hostname, requestPath string) (*BackendBinding, bool) {
	s.mu.RLock()
	defer s.mu.RUnlock()
	bindings, ok := s.bindings[normalizeHost(hostname)]
	if !ok || len(bindings) == 0 {
		return nil, false
	}
	selected := bindings[0]
	for i := range bindings {
		if betterBinding(bindings[i], selected, requestPath) {
			selected = bindings[i]
		}
	}
	copyBinding := *selected
	return &copyBinding, true
}

func (s *memoryStore) Put(binding *BackendBinding) {
	if binding == nil {
		return
	}
	s.mu.Lock()
	defer s.mu.Unlock()
	copyBinding := *binding
	host := normalizeHost(binding.Hostname)
	s.bindings[host] = append(s.bindings[host], &copyBinding)
}

func (s *memoryStore) Delete(hostname string) {
	s.mu.Lock()
	defer s.mu.Unlock()
	delete(s.bindings, normalizeHost(hostname))
}

func (s *memoryStore) Clear() {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.bindings = map[string][]*BackendBinding{}
}

func betterBinding(left, right *BackendBinding, requestPath string) bool {
	if right == nil {
		return true
	}
	leftPriority := parseBindingPriority(left)
	rightPriority := parseBindingPriority(right)
	if leftPriority != rightPriority {
		return leftPriority > rightPriority
	}
	leftPath := parseBindingPath(left)
	rightPath := parseBindingPath(right)
	if len(leftPath) != len(rightPath) {
		return len(leftPath) > len(rightPath)
	}
	return bindingMatchesPath(left, requestPath) && !bindingMatchesPath(right, requestPath)
}

func parseBindingPath(binding *BackendBinding) string {
	if binding == nil {
		return "/"
	}
	if binding.Path == "" {
		return "/"
	}
	return binding.Path
}

func parseBindingPriority(binding *BackendBinding) int32 {
	if binding == nil {
		return 0
	}
	return binding.Priority
}

func bindingMatchesPath(binding *BackendBinding, requestPath string) bool {
	path := parseBindingPath(binding)
	if path == "/" || path == "" {
		return true
	}
	if len(requestPath) < len(path) {
		return false
	}
	return requestPath[:len(path)] == path
}

type RouteLookupReader interface {
	GetDomainEntryByHostname(ctx context.Context, hostname string) (*metadata.DomainEntryProjection, error)
}

type ReplicaReader interface {
	ReadCache() RouteLookupReader
}

func lookupRouteFromReadOnlyCache(ctx context.Context, slave ReplicaReader, hostname string) (*metadata.DomainEntryProjection, error) {
	if slave == nil {
		return nil, fmt.Errorf("slave is nil")
	}
	cache := slave.ReadCache()
	if cache == nil {
		return nil, nil
	}
	return cache.GetDomainEntryByHostname(ctx, hostname)
}
