package manage

import (
	"net/http"
	"strings"
	"sync"
)

// API 暴露 repository manage REST API。
type API struct {
	wrapper *Wrapper
	http01  *http01Registry
}

// NewAPI 创建 API。
func NewAPI(wrapper *Wrapper) *API {
	return &API{wrapper: wrapper, http01: newHTTP01Registry()}
}

// Handler 返回聚合后的 http handler。
func (a *API) Handler() http.Handler {
	mux := http.NewServeMux()
	mux.HandleFunc("/healthz", a.handleHealthz)
	mux.HandleFunc("/.well-known/acme-challenge/", a.handleHTTP01Challenge)
	mux.HandleFunc("/manage/", a.handleResource)
	return mux
}

func (a *API) SetHTTP01Challenge(token, keyAuthorization string) {
	if a == nil || a.http01 == nil {
		return
	}
	a.http01.Set(token, keyAuthorization)
}

func (a *API) DeleteHTTP01Challenge(token string) {
	if a == nil || a.http01 == nil {
		return
	}
	a.http01.Delete(token)
}

func (a *API) handleHealthz(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodGet {
		http.Error(w, "method not allowed", http.StatusMethodNotAllowed)
		return
	}
	w.WriteHeader(http.StatusOK)
	_, _ = w.Write([]byte("ok\n"))
}

func (a *API) handleHTTP01Challenge(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodGet && r.Method != http.MethodHead {
		http.Error(w, "method not allowed", http.StatusMethodNotAllowed)
		return
	}
	token := strings.TrimPrefix(r.URL.Path, "/.well-known/acme-challenge/")
	token = strings.TrimSpace(token)
	if token == "" || strings.Contains(token, "/") {
		http.NotFound(w, r)
		return
	}
	if a == nil || a.http01 == nil {
		http.NotFound(w, r)
		return
	}
	keyAuth, ok := a.http01.Get(token)
	if !ok {
		http.NotFound(w, r)
		return
	}
	w.Header().Set("Content-Type", "text/plain; charset=utf-8")
	w.WriteHeader(http.StatusOK)
	if r.Method != http.MethodHead {
		_, _ = w.Write([]byte(keyAuth))
	}
}

type http01Registry struct {
	mu    sync.RWMutex
	items map[string]string
}

func newHTTP01Registry() *http01Registry {
	return &http01Registry{items: map[string]string{}}
}

func (r *http01Registry) Set(token, keyAuthorization string) {
	r.mu.Lock()
	defer r.mu.Unlock()
	r.items[token] = keyAuthorization
}

func (r *http01Registry) Get(token string) (string, bool) {
	r.mu.RLock()
	defer r.mu.RUnlock()
	value, ok := r.items[token]
	return value, ok
}

func (r *http01Registry) Delete(token string) {
	r.mu.Lock()
	defer r.mu.Unlock()
	delete(r.items, token)
}
