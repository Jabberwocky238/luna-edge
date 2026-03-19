package master

import (
	"net/http"
	"strings"
)

// API 暴露 repository manage REST API。
type API struct {
	http01 HTTP01Registry
}

// NewAPI 创建 API。
func NewAPI(http01 HTTP01Registry) *API {
	return &API{http01: http01}
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

func (a *API) handleResource(w http.ResponseWriter, r *http.Request) {
	http.Error(w, "not implemented", http.StatusNotImplemented)
}
