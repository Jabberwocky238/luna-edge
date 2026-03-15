package ingress

import (
	"crypto/tls"
	"crypto/x509"
	"os"
	"path/filepath"
	"testing"
	"time"
)

func TestTLSCertResolverLoadsCertificateFromLocalPathAndCachesIt(t *testing.T) {
	cacheRoot := t.TempDir()
	serverName := "readonly.example.com"
	rootCA := writeTestCertificate(t, cacheRoot, serverName)
	if rootCA == nil {
		t.Fatal("expected test root CA")
	}

	resolver, err := NewLunaTLSCertResolver(cacheRoot, 2)
	if err != nil {
		t.Fatalf("create resolver: %v", err)
	}
	cert, err := resolver.Load(serverName)
	if err != nil {
		t.Fatalf("load certificate: %v", err)
	}
	if cert == nil {
		t.Fatal("expected certificate to be loaded")
	}
	if _, ok := resolver.certs.Get(serverName); !ok {
		t.Fatal("expected certificate to be cached in lru after load")
	}
}

func TestTLSCertResolverFallsBackToWildcardCertificate(t *testing.T) {
	cacheRoot := t.TempDir()
	rootCA := writeTestCertificate(t, cacheRoot, "*.a.com")
	if rootCA == nil {
		t.Fatal("expected test root CA")
	}

	resolver, err := NewLunaTLSCertResolver(cacheRoot, 2)
	if err != nil {
		t.Fatalf("create resolver: %v", err)
	}
	cert, err := resolver.Load("aaa.a.com")
	if err != nil {
		t.Fatalf("load wildcard certificate: %v", err)
	}
	if cert == nil {
		t.Fatal("expected wildcard certificate to be loaded")
	}
	if _, ok := resolver.certs.Get("*.a.com"); !ok {
		t.Fatal("expected wildcard certificate to be cached under wildcard hostname")
	}
}

func TestTLSCertResolverPrefersExactCertificateOverWildcard(t *testing.T) {
	cacheRoot := t.TempDir()
	writeTestCertificate(t, cacheRoot, "*.a.com")
	rootCA := writeTestCertificate(t, cacheRoot, "aaa.a.com")
	if rootCA == nil {
		t.Fatal("expected test root CA")
	}

	resolver, err := NewLunaTLSCertResolver(cacheRoot, 2)
	if err != nil {
		t.Fatalf("create resolver: %v", err)
	}
	cert, err := resolver.Load("aaa.a.com")
	if err != nil {
		t.Fatalf("load exact certificate: %v", err)
	}
	if cert == nil || len(cert.Certificate) == 0 {
		t.Fatal("expected exact certificate to be loaded")
	}
	if _, ok := resolver.certs.Get("aaa.a.com"); !ok {
		t.Fatal("expected exact certificate to be cached under exact hostname")
	}
}

func TestTLSCertResolverUsesFilesystemSafeWildcardDirectory(t *testing.T) {
	cacheRoot := t.TempDir()
	writeTestCertificate(t, cacheRoot, "*.a.com")

	crtPath := filepath.Join(cacheRoot, ".wildcard", "a.com", "tls.crt")
	keyPath := filepath.Join(cacheRoot, ".wildcard", "a.com", "tls.key")
	if !fileExists(crtPath) || !fileExists(keyPath) {
		t.Fatalf("expected wildcard cert files in safe directory, got %q and %q", crtPath, keyPath)
	}
}

func TestTLSCertResolverDeleteCandidatesPromotesExactCertificateAfterWildcardCache(t *testing.T) {
	cacheRoot := t.TempDir()
	writeTestCertificate(t, cacheRoot, "*.a.com")

	resolver, err := NewLunaTLSCertResolver(cacheRoot, 4)
	if err != nil {
		t.Fatalf("create resolver: %v", err)
	}
	cert, err := resolver.Load("aaa.a.com")
	if err != nil {
		t.Fatalf("load wildcard certificate: %v", err)
	}
	if got := certificateCommonName(t, cert); got != "*.a.com" {
		t.Fatalf("expected wildcard cert before refresh, got %q", got)
	}

	writeTestCertificate(t, cacheRoot, "aaa.a.com")
	resolver.DeleteCandidates("aaa.a.com")

	cert, err = resolver.Load("aaa.a.com")
	if err != nil {
		t.Fatalf("load exact certificate after refresh: %v", err)
	}
	if got := certificateCommonName(t, cert); got != "aaa.a.com" {
		t.Fatalf("expected exact cert after refresh, got %q", got)
	}
}

func TestTLSCertResolverRejectsUnsafeHostname(t *testing.T) {
	cacheRoot := t.TempDir()
	resolver, err := NewLunaTLSCertResolver(cacheRoot, 2)
	if err != nil {
		t.Fatalf("create resolver: %v", err)
	}

	if _, err := resolver.Load("../../etc/passwd"); err == nil {
		t.Fatal("expected unsafe hostname to be rejected")
	}
	if got := sanitizeHostname("../bad.example.com"); got != "" {
		t.Fatalf("expected sanitized hostname to be empty, got %q", got)
	}
}

func TestTLSCertResolverAutomaticallyRefreshesOnCertificateChange(t *testing.T) {
	cacheRoot := t.TempDir()
	writeTestCertificate(t, cacheRoot, "watch.example.com")

	resolver, err := NewLunaTLSCertResolver(cacheRoot, 4)
	if err != nil {
		t.Fatalf("create resolver: %v", err)
	}
	cert, err := resolver.Load("watch.example.com")
	if err != nil {
		t.Fatalf("load initial certificate: %v", err)
	}
	if got := certificateCommonName(t, cert); got != "watch.example.com" {
		t.Fatalf("expected initial certificate CN, got %q", got)
	}

	if err := os.Remove(filepath.Join(cacheRoot, "watch.example.com", "tls.crt")); err != nil {
		t.Fatalf("remove certificate: %v", err)
	}
	if err := os.Remove(filepath.Join(cacheRoot, "watch.example.com", "tls.key")); err != nil {
		t.Fatalf("remove key: %v", err)
	}

	waitForCondition(t, 3*time.Second, func() bool {
		_, ok := resolver.certs.Get("watch.example.com")
		return !ok
	})

	if _, err := resolver.Load("watch.example.com"); err == nil {
		t.Fatal("expected load to fail after certificate removal")
	}

	writeTestCertificate(t, cacheRoot, "watch.example.com")
	waitForCondition(t, 3*time.Second, func() bool {
		_, ok := resolver.certs.Get("watch.example.com")
		return !ok
	})

	cert, err = resolver.Load("watch.example.com")
	if err != nil {
		t.Fatalf("reload certificate after rewrite: %v", err)
	}
	if got := certificateCommonName(t, cert); got != "watch.example.com" {
		t.Fatalf("expected reloaded cert after invalidation, got %q", got)
	}
}

func TestTLSCertResolverWatcherDoesNotPreloadNewCertificate(t *testing.T) {
	cacheRoot := t.TempDir()
	resolver, err := NewLunaTLSCertResolver(cacheRoot, 4)
	if err != nil {
		t.Fatalf("create resolver: %v", err)
	}
	if _, ok := resolver.certs.Get("live.example.com"); ok {
		t.Fatal("expected empty cache before filesystem event")
	}

	writeTestCertificate(t, cacheRoot, "live.example.com")
	time.Sleep(200 * time.Millisecond)

	if _, ok := resolver.certs.Get("live.example.com"); ok {
		t.Fatal("expected watcher to invalidate only, not preload cache")
	}

	cert, err := resolver.Load("live.example.com")
	if err != nil {
		t.Fatalf("load certificate after create: %v", err)
	}
	if got := certificateCommonName(t, cert); got != "live.example.com" {
		t.Fatalf("expected on-demand loaded cert, got %q", got)
	}
}

func TestTLSCertResolverReturnsErrorForInvalidCertRoot(t *testing.T) {
	file := filepath.Join(t.TempDir(), "not-a-dir")
	if err := os.WriteFile(file, []byte("x"), 0o644); err != nil {
		t.Fatalf("write file: %v", err)
	}

	resolver, err := NewLunaTLSCertResolver(file, 2)
	if err == nil {
		t.Fatalf("expected invalid cert root to fail, got resolver=%v", resolver)
	}
}

func certificateCommonName(t *testing.T, cert *tls.Certificate) string {
	t.Helper()
	if cert == nil || len(cert.Certificate) == 0 {
		t.Fatal("expected parsed certificate bytes")
	}
	leaf, err := x509.ParseCertificate(cert.Certificate[0])
	if err != nil {
		t.Fatalf("parse leaf certificate: %v", err)
	}
	return leaf.Subject.CommonName
}

func waitForCondition(t *testing.T, timeout time.Duration, cond func() bool) {
	t.Helper()
	deadline := time.Now().Add(timeout)
	for time.Now().Before(deadline) {
		if cond() {
			return
		}
		time.Sleep(25 * time.Millisecond)
	}
	t.Fatal("condition was not met before timeout")
}
