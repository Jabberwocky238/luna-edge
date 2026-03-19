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

type RouteLookupReader interface {
	GetDomainEntryByHostname(ctx context.Context, hostname string) (*metadata.DomainEntryProjection, error)
}

type ReplicaReader interface {
	ReadCache() RouteLookupReader
}

func newMemoryStore() *memoryStore {
	return &memoryStore{
		bindings: map[string][]*BackendBinding{},
	}
}

func (s *memoryStore) Get(hostname, requestPath string) (*BackendBinding, bool) {
	s.mu.RLock()
	defer s.mu.RUnlock()
	return s.selectLocked(normalizeHost(hostname), requestPath, nil)
}

func (s *memoryStore) GetByProtocol(hostname, requestPath string, protocols ...RouteKind) (*BackendBinding, bool) {
	s.mu.RLock()
	defer s.mu.RUnlock()

	allowed := make(map[RouteKind]struct{}, len(protocols))
	for _, protocol := range protocols {
		allowed[protocol] = struct{}{}
	}
	return s.selectLocked(normalizeHost(hostname), requestPath, allowed)
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

func (s *memoryStore) selectLocked(hostname, requestPath string, allowed map[RouteKind]struct{}) (*BackendBinding, bool) {
	bindings, ok := s.bindings[hostname]
	if !ok || len(bindings) == 0 {
		return nil, false
	}

	var selected *BackendBinding
	for i := range bindings {
		candidate := bindings[i]
		if allowed != nil {
			if _, ok := allowed[candidate.Protocol]; !ok {
				continue
			}
		}
		if selected == nil || betterBinding(candidate, selected, requestPath) {
			selected = candidate
		}
	}
	if selected == nil {
		return nil, false
	}
	copyBinding := *selected
	return &copyBinding, true
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
