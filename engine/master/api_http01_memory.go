package master

import "sync"

type memoryHTTP01Registry struct {
	mu    sync.RWMutex
	items map[string]string
}

func newMemoryHTTP01Registry() *memoryHTTP01Registry {
	return &memoryHTTP01Registry{items: map[string]string{}}
}

func (r *memoryHTTP01Registry) Set(token, keyAuthorization string) {
	r.mu.Lock()
	defer r.mu.Unlock()
	r.items[token] = keyAuthorization
}

func (r *memoryHTTP01Registry) Get(token string) (string, bool) {
	r.mu.RLock()
	defer r.mu.RUnlock()
	value, ok := r.items[token]
	return value, ok
}

func (r *memoryHTTP01Registry) Delete(token string) {
	r.mu.Lock()
	defer r.mu.Unlock()
	delete(r.items, token)
}
