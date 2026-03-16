package ingress

import (
	"context"
	"encoding/json"
	"fmt"
	"sync"

	enginepkg "github.com/jabberwocky238/luna-edge/engine"
	"github.com/jabberwocky238/luna-edge/repository/metadata"
)

type memoryStore struct {
	mu       sync.RWMutex
	bindings map[string][]*metadata.ServiceBinding
}

func newMemoryStore() *memoryStore {
		return &memoryStore{
		bindings: map[string][]*metadata.ServiceBinding{},
	}
}

func (s *memoryStore) Get(hostname, requestPath string) (*metadata.ServiceBinding, bool) {
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

func (s *memoryStore) Put(binding *metadata.ServiceBinding) {
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
	s.bindings = map[string][]*metadata.ServiceBinding{}
}

func betterBinding(left, right *metadata.ServiceBinding, requestPath string) bool {
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

func parseBindingPath(binding *metadata.ServiceBinding) string {
	if binding == nil {
		return "/"
	}
	var payload struct {
		Path string `json:"path"`
	}
	_ = json.Unmarshal([]byte(binding.BackendJSON), &payload)
	if payload.Path == "" {
		return "/"
	}
	return payload.Path
}

func parseBindingPriority(binding *metadata.ServiceBinding) int32 {
	if binding == nil {
		return 0
	}
	var payload struct {
		Priority int32 `json:"priority"`
	}
	_ = json.Unmarshal([]byte(binding.BackendJSON), &payload)
	return payload.Priority
}

func bindingMatchesPath(binding *metadata.ServiceBinding, requestPath string) bool {
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
	GetRouteByHostname(ctx context.Context, hostname string) (*enginepkg.RouteRecord, error)
}

type ReplicaReader interface {
	ReadCache() RouteLookupReader
}

func lookupRouteFromReadOnlyCache(ctx context.Context, slave ReplicaReader, hostname string) (*enginepkg.RouteRecord, error) {
	if slave == nil {
		return nil, fmt.Errorf("slave is nil")
	}
	cache := slave.ReadCache()
	if cache == nil {
		return nil, fmt.Errorf("slave cache is nil")
	}
	return cache.GetRouteByHostname(ctx, hostname)
}
