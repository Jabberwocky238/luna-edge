package manage

import (
	"io"
	"net/http"
	"net/http/httptest"
	"testing"
)

func TestAPIHTTP01Challenge(t *testing.T) {
	t.Parallel()

	api := NewAPI(nil)
	api.SetHTTP01Challenge("token-1", "token-1.key-auth")

	req := httptest.NewRequest(http.MethodGet, "/.well-known/acme-challenge/token-1", nil)
	rec := httptest.NewRecorder()
	api.Handler().ServeHTTP(rec, req)

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

	api.DeleteHTTP01Challenge("token-1")

	req = httptest.NewRequest(http.MethodGet, "/.well-known/acme-challenge/token-1", nil)
	rec = httptest.NewRecorder()
	api.Handler().ServeHTTP(rec, req)
	if rec.Result().StatusCode != http.StatusNotFound {
		t.Fatalf("expected 404 after cleanup, got %d", rec.Result().StatusCode)
	}
}

