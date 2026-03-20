package ingress

import (
	"context"
	"io"
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/jabberwocky238/luna-edge/repository/metadata"
)

func TestEngineStripsMatchedPathPrefixBeforeProxying(t *testing.T) {
	t.Parallel()

	var gotPath string
	var gotRawQuery string
	upstream := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		gotPath = r.URL.Path
		gotRawQuery = r.URL.RawQuery
		w.WriteHeader(http.StatusOK)
		_, _ = w.Write([]byte("ok"))
	}))
	defer upstream.Close()

	engine, err := NewEngine(EngineOptions{HTTPListenAddr: "127.0.0.1:0"}, nil)
	if err != nil {
		t.Fatalf("new engine: %v", err)
	}
	engine.InjectRepository(staticProjectionReader{entry: &metadata.DomainEntryProjection{
		Hostname: "demo.example.com",
		HTTPRoutes: []metadata.HTTPRouteProjection{{
			ID:       "route-ingress",
			Path:     "/ingress",
			Priority: 1,
			BackendRef: &metadata.ServiceBackendRef{
				ID:                "backend-1",
				Type:              metadata.ServiceBackendTypeExternal,
				ArbitraryEndpoint: upstream.URL,
			},
		}},
	}})

	tests := []struct {
		name         string
		requestURI   string
		expectedPath string
		expectedQS   string
	}{
		{name: "prefix root", requestURI: "http://demo.example.com/ingress", expectedPath: "/", expectedQS: ""},
		{name: "prefix child", requestURI: "http://demo.example.com/ingress/api/v1?q=1", expectedPath: "/api/v1", expectedQS: "q=1"},
	}

	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			req := httptest.NewRequest(http.MethodGet, tc.requestURI, nil)
			req.Host = "demo.example.com"
			rec := httptest.NewRecorder()

			engine.NewHTTPHandler().ServeHTTP(rec, req)

			resp := rec.Result()
			defer resp.Body.Close()
			if resp.StatusCode != http.StatusOK {
				body, _ := io.ReadAll(resp.Body)
				t.Fatalf("unexpected status: %d body=%s", resp.StatusCode, string(body))
			}
			if gotPath != tc.expectedPath {
				t.Fatalf("unexpected upstream path: got %q want %q", gotPath, tc.expectedPath)
			}
			if gotRawQuery != tc.expectedQS {
				t.Fatalf("unexpected upstream raw query: got %q want %q", gotRawQuery, tc.expectedQS)
			}
		})
	}
}

type staticProjectionReader struct {
	entry *metadata.DomainEntryProjection
}

func (s staticProjectionReader) GetDomainEntryProjectionByDomain(_ context.Context, _ string) (*metadata.DomainEntryProjection, error) {
	return s.entry, nil
}
