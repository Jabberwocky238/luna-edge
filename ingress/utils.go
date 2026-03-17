package ingress

import (
	"crypto/rand"
	"crypto/rsa"
	"crypto/tls"
	"crypto/x509"
	"crypto/x509/pkix"
	"encoding/pem"
	"fmt"
	"math/big"
	"net"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"regexp"
	"strings"
	"testing"
	"time"

	"k8s.io/apimachinery/pkg/api/meta"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/runtime"
	"k8s.io/client-go/tools/cache"
)

var hostnameLabelPattern = regexp.MustCompile(`^[a-z0-9](?:[a-z0-9-]{0,61}[a-z0-9])?$`)

func normalizeHost(host string) string {
	host = strings.TrimSpace(strings.ToLower(host))
	host = strings.TrimPrefix(host, "http://")
	host = strings.TrimPrefix(host, "https://")
	if idx := strings.Index(host, ":"); idx >= 0 {
		host = host[:idx]
	}
	return sanitizeHostname(host)
}

func sanitizeHostname(host string) string {
	host = strings.TrimSpace(strings.ToLower(host))
	if host == "" {
		return ""
	}

	wildcard := false
	if strings.HasPrefix(host, "*.") {
		wildcard = true
		host = strings.TrimPrefix(host, "*.")
	}
	if host == "" || strings.Contains(host, "..") || strings.ContainsAny(host, `/\ `) {
		return ""
	}

	labels := strings.Split(host, ".")
	for _, label := range labels {
		if !hostnameLabelPattern.MatchString(label) {
			return ""
		}
	}

	if wildcard {
		return "*." + host
	}
	return host
}

func buildUpstreamURL(protocol, address string, port uint32) string {
	protocol = strings.TrimSpace(strings.ToLower(protocol))
	if protocol == "" {
		protocol = "http"
	}
	address = strings.TrimSpace(address)
	if address == "" {
		return ""
	}
	if strings.HasPrefix(address, "http://") || strings.HasPrefix(address, "https://") {
		return address
	}
	if strings.Contains(address, ":") {
		return fmt.Sprintf("%s://%s", protocol, address)
	}
	if port > 0 {
		return fmt.Sprintf("%s://%s:%d", protocol, address, port)
	}
	return fmt.Sprintf("%s://%s", protocol, address)
}

func fileExists(path string) bool {
	info, err := os.Stat(path)
	if err != nil {
		return false
	}
	return !info.IsDir()
}

func resolveCertLRUSize(size int) int {
	if size <= 0 {
		return DefaultIngressLRUSize
	}
	return size
}

func initBridgeHandlers(bridge *K8sBridge) {
	bridge.ensureIngressInformer()
	if bridge.ingressFactory != nil {
		bridge.ingressFactory.Networking().V1().Ingresses().Informer().AddEventHandler(cache.ResourceEventHandlerFuncs{
			AddFunc: func(obj interface{}) {
				bridge.storeIngress(obj)
			},
			UpdateFunc: func(_, newObj interface{}) {
				bridge.storeIngress(newObj)
			},
			DeleteFunc: func(obj interface{}) {
				bridge.deleteIngress(obj)
			},
		})
	}

	bridge.ensureGatewayInformers()
}

func deleteByNamespaceName(obj interface{}, deleter func(namespace, name string)) {
	switch value := obj.(type) {
	case metav1.Object:
		deleter(value.GetNamespace(), value.GetName())
	case cache.DeletedFinalStateUnknown:
		accessor, ok := value.Obj.(metav1.Object)
		if ok && accessor != nil {
			deleter(accessor.GetNamespace(), accessor.GetName())
			return
		}
		ro, ok := value.Obj.(runtime.Object)
		if !ok || ro == nil {
			return
		}
		accessor, err := meta.Accessor(ro)
		if err == nil {
			deleter(accessor.GetNamespace(), accessor.GetName())
		}
	}
}

func buildServiceAddress(name, namespace string) string {
	return fmt.Sprintf("%s.%s.svc.cluster.local", name, namespace)
}

func freeAddr(t *testing.T) string {
	t.Helper()
	ln, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatalf("listen free addr: %v", err)
	}
	addr := ln.Addr().String()
	_ = ln.Close()
	return addr
}

func splitHostPort(t *testing.T, addr string) (string, uint32) {
	t.Helper()
	host, portStr, err := net.SplitHostPort(addr)
	if err != nil {
		t.Fatalf("split host port: %v", err)
	}
	portNum, err := net.LookupPort("tcp", portStr)
	if err != nil {
		t.Fatalf("lookup port: %v", err)
	}
	return host, uint32(portNum)
}

func writeTestCertificate(t *testing.T, certRoot, hostname string) *x509.CertPool {
	t.Helper()
	priv, err := rsa.GenerateKey(rand.Reader, 2048)
	if err != nil {
		t.Fatalf("generate key: %v", err)
	}
	template := &x509.Certificate{
		SerialNumber:          big.NewInt(1),
		Subject:               pkix.Name{CommonName: hostname},
		NotBefore:             time.Now().Add(-time.Hour),
		NotAfter:              time.Now().Add(24 * time.Hour),
		KeyUsage:              x509.KeyUsageDigitalSignature | x509.KeyUsageKeyEncipherment,
		ExtKeyUsage:           []x509.ExtKeyUsage{x509.ExtKeyUsageServerAuth},
		BasicConstraintsValid: true,
		DNSNames:              []string{hostname},
	}
	der, err := x509.CreateCertificate(rand.Reader, template, template, &priv.PublicKey, priv)
	if err != nil {
		t.Fatalf("create cert: %v", err)
	}
	dir := filepath.Join(certRoot, certificateDirectoryName(hostname))
	if err := os.MkdirAll(dir, 0o755); err != nil {
		t.Fatalf("mkdir cert dir: %v", err)
	}
	crtPath := filepath.Join(dir, "tls.crt")
	keyPath := filepath.Join(dir, "tls.key")
	if err := os.WriteFile(crtPath, pem.EncodeToMemory(&pem.Block{Type: "CERTIFICATE", Bytes: der}), 0o644); err != nil {
		t.Fatalf("write cert: %v", err)
	}
	keyBytes := x509.MarshalPKCS1PrivateKey(priv)
	if err := os.WriteFile(keyPath, pem.EncodeToMemory(&pem.Block{Type: "RSA PRIVATE KEY", Bytes: keyBytes}), 0o600); err != nil {
		t.Fatalf("write key: %v", err)
	}
	pool := x509.NewCertPool()
	cert, err := x509.ParseCertificate(der)
	if err != nil {
		t.Fatalf("parse cert: %v", err)
	}
	pool.AddCert(cert)
	return pool
}

func metav1ObjectMeta(namespace, name string, generation int64) metav1.ObjectMeta {
	return metav1.ObjectMeta{Namespace: namespace, Name: name, Generation: generation}
}

func waitForHTTPServer(t *testing.T, rawURL string) {
	t.Helper()
	client := &http.Client{Timeout: 200 * time.Millisecond}
	deadline := time.Now().Add(3 * time.Second)
	for time.Now().Before(deadline) {
		resp, err := client.Get(rawURL)
		if err == nil {
			_ = resp.Body.Close()
			return
		}
		time.Sleep(50 * time.Millisecond)
	}
	t.Fatalf("http server did not become ready: %s", rawURL)
}

func waitForTLSServer(t *testing.T, addr, serverName string, rootCA *x509.CertPool) {
	t.Helper()
	deadline := time.Now().Add(3 * time.Second)
	for time.Now().Before(deadline) {
		conn, err := tls.Dial("tcp", addr, &tls.Config{ServerName: serverName, RootCAs: rootCA})
		if err == nil {
			_ = conn.Close()
			return
		}
		time.Sleep(50 * time.Millisecond)
	}
	t.Fatalf("tls server did not become ready: %s", addr)
}

func requireSocketSupport(t *testing.T) {
	t.Helper()
	ln, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Skipf("socket listen not available in this environment: %v", err)
		return
	}
	_ = ln.Close()
}

func newLocalTestServer(t *testing.T, handler http.Handler) *httptest.Server {
	t.Helper()
	ln, err := net.Listen("tcp4", "127.0.0.1:0")
	if err != nil {
		t.Fatalf("listen local test server: %v", err)
	}
	srv := httptest.NewUnstartedServer(handler)
	srv.Listener = ln
	srv.Start()
	return srv
}
