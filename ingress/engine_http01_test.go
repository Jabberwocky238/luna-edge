package ingress

import (
	"io"
	"net/http"
	"net/http/httptest"
	"testing"
)

func TestEngineProxiesACMEHTTP01ToMaster(t *testing.T) {
	t.Parallel()

	master := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path != "/.well-known/acme-challenge/token-1" {
			t.Fatalf("unexpected path: %s", r.URL.Path)
		}
		w.WriteHeader(http.StatusOK)
		_, _ = w.Write([]byte("token-1.key-auth"))
	}))
	defer master.Close()

	engine, err := NewEngine(EngineOptions{
		HTTPListenAddr:       "127.0.0.1:0",
		MasterHTTP01ProxyURL: master.URL,
	}, nil)
	if err != nil {
		t.Fatalf("new engine: %v", err)
	}

	req := httptest.NewRequest(http.MethodGet, "http://edge.example/.well-known/acme-challenge/token-1", nil)
	req.Host = "app.example.com"
	rec := httptest.NewRecorder()

	engine.NewHTTPHandler().ServeHTTP(rec, req)

	resp := rec.Result()
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		t.Fatalf("unexpected status: %d", resp.StatusCode)
	}
	body, err := io.ReadAll(resp.Body)
	if err != nil {
		t.Fatalf("read body: %v", err)
	}
	if string(body) != "token-1.key-auth" {
		t.Fatalf("unexpected body: %q", string(body))
	}
}

