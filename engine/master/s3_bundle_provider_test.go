package master

import (
	"bufio"
	"context"
	"fmt"
	"io"
	"net/http"
	"net/http/httptest"
	"path/filepath"
	"strconv"
	"strings"
	"testing"
	"time"

	enginepkg "github.com/jabberwocky238/luna-edge/engine"
	"github.com/jabberwocky238/luna-edge/repository"
	"github.com/jabberwocky238/luna-edge/repository/connection"
	"github.com/jabberwocky238/luna-edge/repository/metadata"
)

func TestS3CertificateBundleProviderFetchesAndStoresObjects(t *testing.T) {
	t.Parallel()

	objects := map[string][]byte{
		"/cert-bucket/bundles/app.example.com/tls.crt":       []byte("crt-bytes"),
		"/cert-bucket/bundles/app.example.com/tls.key":       []byte("key-bytes"),
		"/cert-bucket/bundles/app.example.com/metadata.json": []byte(`{"hostname":"app.example.com","revision":3}`),
	}
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Header.Get("Authorization") == "" {
			t.Errorf("expected Authorization header")
		}
		if strings.HasPrefix(r.URL.Path, "/cert-bucket") && (r.Method == http.MethodGet || r.Method == http.MethodHead) && strings.Contains(r.URL.RawQuery, "location") {
			w.WriteHeader(http.StatusOK)
			return
		}
		switch {
		case r.Method == http.MethodGet:
			body, ok := objects[r.URL.Path]
			if !ok {
				http.NotFound(w, r)
				return
			}
			w.Header().Set("Last-Modified", time.Now().UTC().Format(http.TimeFormat))
			_, _ = w.Write(body)
		case r.Method == http.MethodPut:
			body := readAllTest(t, r)
			if strings.EqualFold(r.Header.Get("X-Amz-Content-Sha256"), "STREAMING-AWS4-HMAC-SHA256-PAYLOAD") {
				var err error
				body, err = decodeAWSChunked(body)
				if err != nil {
					t.Fatalf("decode aws chunked body: %v", err)
				}
			}
			objects[r.URL.Path] = body
			w.Header().Set("ETag", `"test-etag"`)
			w.WriteHeader(http.StatusOK)
		default:
			http.Error(w, "unexpected method", http.StatusMethodNotAllowed)
		}
	}))
	defer server.Close()

	repo := newBundleTestRepository(t)
	seedBundleLocation(t, repo)
	ctx := context.Background()

	provider, err := NewS3CertificateBundleProvider(repo, S3Config{
		Endpoint:        server.URL,
		Region:          "us-east-1",
		AccessKeyID:     "test-access",
		SecretAccessKey: "test-secret",
		UsePathStyle:    true,
		HTTPTimeout:     time.Second,
	})
	if err != nil {
		t.Fatalf("new s3 bundle provider: %v", err)
	}

	bundle, err := provider.FetchCertificateBundle(ctx, "app.example.com", 3)
	if err != nil {
		t.Fatalf("fetch certificate bundle: %v", err)
	}
	if string(bundle.TLSCrt) != "crt-bytes" {
		t.Fatalf("unexpected crt bytes: %q", string(bundle.TLSCrt))
	}
	if string(bundle.TLSKey) != "key-bytes" {
		t.Fatalf("unexpected key bytes: %q", string(bundle.TLSKey))
	}

	updated := &enginepkg.CertificateBundle{
		Hostname:     "app.example.com",
		Revision:     3,
		TLSCrt:       []byte("updated-crt"),
		TLSKey:       []byte("updated-key"),
		MetadataJSON: []byte(`{"hostname":"app.example.com","revision":3,"updated":true}`),
	}
	if err := provider.PutCertificateBundle(ctx, "app.example.com", 3, updated); err != nil {
		t.Fatalf("put certificate bundle: %v", err)
	}

	if got := string(objects["/cert-bucket/bundles/app.example.com/tls.crt"]); got != "updated-crt" {
		t.Fatalf("unexpected updated crt: %q", got)
	}
	if got := string(objects["/cert-bucket/bundles/app.example.com/tls.key"]); got != "updated-key" {
		t.Fatalf("unexpected updated key: %q", got)
	}
	if got := string(objects["/cert-bucket/bundles/app.example.com/metadata.json"]); !strings.Contains(got, `"updated":true`) {
		t.Fatalf("unexpected updated metadata: %q", got)
	}
}

func newBundleTestRepository(t *testing.T) repository.Repository {
	t.Helper()
	factory, err := repository.NewFactory(connection.Config{
		Driver:      connection.DriverSQLite,
		Path:        filepath.Join(t.TempDir(), "master.db"),
		AutoMigrate: true,
	})
	if err != nil {
		t.Fatalf("new repository factory: %v", err)
	}
	t.Cleanup(func() { _ = factory.Close() })
	return factory.Repository()
}

func seedBundleLocation(t *testing.T, repo repository.Repository) {
	t.Helper()
	ctx := context.Background()
	mustUpsertBundleResource(t, repo.DomainEndpoints().UpsertResource(ctx, &metadata.DomainEndpoint{
		ID:          "domain-1",
		Hostname:    "app.example.com",
		BackendType: metadata.BackendTypeL7HTTP,
		CertID:      "cert-1",
	}))
	mustUpsertBundleResource(t, repo.CertificateRevisions().UpsertResource(ctx, &metadata.CertificateRevision{
		ID:               "cert-1",
		DomainEndpointID: "domain-1",
		Hostname:         "app.example.com",
		Revision:         3,
		ArtifactBucket:   "cert-bucket",
		ArtifactPrefix:   "bundles/app.example.com",
	}))
}

func readAllTest(t *testing.T, r *http.Request) []byte {
	t.Helper()
	defer func() { _ = r.Body.Close() }()
	body, err := io.ReadAll(r.Body)
	if err != nil {
		t.Fatal(err)
	}
	return body
}

func mustUpsertBundleResource(t *testing.T, err error) {
	t.Helper()
	if err != nil {
		t.Fatal(err)
	}
}

func decodeAWSChunked(body []byte) ([]byte, error) {
	reader := bufio.NewReader(strings.NewReader(string(body)))
	var out []byte
	for {
		line, err := reader.ReadString('\n')
		if err != nil {
			return nil, err
		}
		line = strings.TrimSpace(line)
		sizeHex, _, _ := strings.Cut(line, ";")
		size, err := strconv.ParseInt(sizeHex, 16, 64)
		if err != nil {
			return nil, fmt.Errorf("parse chunk size %q: %w", sizeHex, err)
		}
		if size == 0 {
			return out, nil
		}
		chunk := make([]byte, size)
		if _, err := io.ReadFull(reader, chunk); err != nil {
			return nil, err
		}
		out = append(out, chunk...)
		if _, err := reader.ReadString('\n'); err != nil {
			return nil, err
		}
	}
}
