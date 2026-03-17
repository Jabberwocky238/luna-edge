package ingress

import (
	"context"
	"crypto/tls"
	"net"
	"net/http"
	"testing"
	"time"

	"github.com/jabberwocky238/luna-edge/repository/metadata"
	networkingv1 "k8s.io/api/networking/v1"
	"k8s.io/client-go/kubernetes/fake"
)

type routeLookupReaderStub struct {
	entry *metadata.DomainEntryProjection
}

func (s routeLookupReaderStub) GetDomainEntryByHostname(context.Context, string) (*metadata.DomainEntryProjection, error) {
	return s.entry, nil
}

type replicaReaderStub struct {
	reader routeLookupReaderStub
}

func (s replicaReaderStub) ReadCache() RouteLookupReader {
	return s.reader
}

func testResolver(t *testing.T) *LunaTLSCertResolver {
	t.Helper()
	resolver, err := NewLunaTLSCertResolver(t.TempDir(), 8)
	if err != nil {
		t.Fatalf("new test resolver: %v", err)
	}
	return resolver
}

func TestEngineHTTPProxyToService(t *testing.T) {
	requireSocketSupport(t)
	upstream := newLocalTestServer(t, http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusOK)
		_, _ = w.Write([]byte("ok-http"))
	}))
	defer upstream.Close()

	host, port := splitHostPort(t, upstream.Listener.Addr().String())

	engine, err := NewEngine(EngineOptions{HTTPListenAddr: freeAddr(t)}, nil)
	if err != nil {
		t.Fatalf("new engine: %v", err)
	}
	engine.memory.Put(&BackendBinding{
		ID:       "binding-http",
		Hostname: "app.example.com",
		Address:  host,
		Port:     port,
		Protocol: RouteKindHTTP,
	})
	if err := engine.Listen(); err != nil {
		t.Fatalf("listen: %v", err)
	}
	defer func() { _ = engine.Stop(context.Background()) }()
	waitForHTTPServer(t, "http://"+engine.opts.HTTPListenAddr+"/healthz")

	req, err := http.NewRequest(http.MethodGet, "http://"+engine.opts.HTTPListenAddr+"/", nil)
	if err != nil {
		t.Fatalf("new request: %v", err)
	}
	req.Host = "app.example.com"

	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		t.Fatalf("http do: %v", err)
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		t.Fatalf("unexpected status: %d", resp.StatusCode)
	}
}

func TestEngineTLSOffloadProxyToService(t *testing.T) {
	requireSocketSupport(t)
	certRoot := t.TempDir()
	serverName := "tls.example.com"
	rootCA := writeTestCertificate(t, certRoot, serverName)

	upstream := newLocalTestServer(t, http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Header.Get("X-Forwarded-Proto") != "https" {
			t.Errorf("expected X-Forwarded-Proto=https, got %q", r.Header.Get("X-Forwarded-Proto"))
		}
		w.WriteHeader(http.StatusOK)
		_, _ = w.Write([]byte("ok-tls"))
	}))
	defer upstream.Close()

	host, port := splitHostPort(t, upstream.Listener.Addr().String())

	resolver, err := NewLunaTLSCertResolver(certRoot, 8)
	if err != nil {
		t.Fatalf("new resolver: %v", err)
	}
	engine, err := NewEngine(EngineOptions{
		TLSListenAddr: freeAddr(t),
	}, resolver)
	if err != nil {
		t.Fatalf("new engine: %v", err)
	}
	engine.memory.Put(&BackendBinding{
		ID:       "binding-tls",
		Hostname: serverName,
		Address:  host,
		Port:     port,
		Protocol: RouteKindHTTP,
	})
	if err := engine.Listen(); err != nil {
		t.Fatalf("listen: %v", err)
	}
	defer func() { _ = engine.Stop(context.Background()) }()
	waitForTLSServer(t, engine.opts.TLSListenAddr, serverName, rootCA)

	client := &http.Client{
		Transport: &http.Transport{
			TLSClientConfig: &tls.Config{
				ServerName: serverName,
				RootCAs:    rootCA,
			},
		},
	}
	resp, err := client.Get("https://" + engine.opts.TLSListenAddr + "/")
	if err != nil {
		t.Fatalf("https get: %v", err)
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		t.Fatalf("unexpected status: %d", resp.StatusCode)
	}
}

func TestEngineRejectUnknownHostAndSNI(t *testing.T) {
	requireSocketSupport(t)
	certRoot := t.TempDir()
	knownServerName := "known.example.com"
	rootCA := writeTestCertificate(t, certRoot, knownServerName)

	resolver, err := NewLunaTLSCertResolver(certRoot, 8)
	if err != nil {
		t.Fatalf("new resolver: %v", err)
	}
	engine, err := NewEngine(EngineOptions{
		HTTPListenAddr: freeAddr(t),
		TLSListenAddr:  freeAddr(t),
	}, resolver)
	if err != nil {
		t.Fatalf("new engine: %v", err)
	}
	if err := engine.Listen(); err != nil {
		t.Fatalf("listen: %v", err)
	}
	defer func() { _ = engine.Stop(context.Background()) }()
	waitForHTTPServer(t, "http://"+engine.opts.HTTPListenAddr+"/healthz")
	waitForTLSServer(t, engine.opts.TLSListenAddr, knownServerName, rootCA)

	req, err := http.NewRequest(http.MethodGet, "http://"+engine.opts.HTTPListenAddr+"/", nil)
	if err != nil {
		t.Fatalf("new request: %v", err)
	}
	req.Host = "missing.example.com"
	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		t.Fatalf("http do: %v", err)
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusNotFound {
		t.Fatalf("expected 404 for missing host, got %d", resp.StatusCode)
	}

	client := &http.Client{
		Transport: &http.Transport{
			TLSClientConfig: &tls.Config{
				ServerName: "missing.example.com",
				RootCAs:    rootCA,
			},
		},
		Timeout: 2 * time.Second,
	}
	_, err = client.Get("https://" + engine.opts.TLSListenAddr + "/")
	if err == nil {
		t.Fatal("expected tls handshake error for missing sni")
	}
}

func TestEngineListenReturnsHTTPBindError(t *testing.T) {
	requireSocketSupport(t)

	lis, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatalf("listen: %v", err)
	}
	defer func() { _ = lis.Close() }()

	engine, err := NewEngine(EngineOptions{HTTPListenAddr: lis.Addr().String()}, nil)
	if err != nil {
		t.Fatalf("new engine: %v", err)
	}
	if err := engine.Listen(); err == nil {
		t.Fatal("expected listen to fail when http port is already in use")
	}
}

func TestEngineListenReturnsTLSBindError(t *testing.T) {
	requireSocketSupport(t)

	lis, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatalf("listen: %v", err)
	}
	defer func() { _ = lis.Close() }()

	resolver, err := NewLunaTLSCertResolver(t.TempDir(), 8)
	if err != nil {
		t.Fatalf("new resolver: %v", err)
	}
	engine, err := NewEngine(EngineOptions{TLSListenAddr: lis.Addr().String()}, resolver)
	if err != nil {
		t.Fatalf("new engine: %v", err)
	}
	if err := engine.Listen(); err == nil {
		t.Fatal("expected listen to fail when tls port is already in use")
	}
}

func TestNewEngineAppliesDefaultServerTimeouts(t *testing.T) {
	resolver, err := NewLunaTLSCertResolver(t.TempDir(), 8)
	if err != nil {
		t.Fatalf("new resolver: %v", err)
	}
	engine, err := NewEngine(EngineOptions{
		HTTPListenAddr: freeAddr(t),
		TLSListenAddr:  freeAddr(t),
	}, resolver)
	if err != nil {
		t.Fatalf("new engine: %v", err)
	}
	if engine.httpServer.ReadHeaderTimeout != defaultReadHeaderTimeout {
		t.Fatalf("unexpected http ReadHeaderTimeout: %v", engine.httpServer.ReadHeaderTimeout)
	}
	if engine.httpServer.ReadTimeout != defaultReadTimeout {
		t.Fatalf("unexpected http ReadTimeout: %v", engine.httpServer.ReadTimeout)
	}
	if engine.httpServer.WriteTimeout != defaultWriteTimeout {
		t.Fatalf("unexpected http WriteTimeout: %v", engine.httpServer.WriteTimeout)
	}
	if engine.httpServer.IdleTimeout != defaultIdleTimeout {
		t.Fatalf("unexpected http IdleTimeout: %v", engine.httpServer.IdleTimeout)
	}
	if engine.tlsEngine == nil || engine.tlsEngine.server == nil {
		t.Fatal("expected tls engine server to be initialized")
	}
	if engine.tlsEngine.server.ReadHeaderTimeout != defaultReadHeaderTimeout {
		t.Fatalf("unexpected tls ReadHeaderTimeout: %v", engine.tlsEngine.server.ReadHeaderTimeout)
	}
	if engine.tlsEngine.server.ReadTimeout != defaultReadTimeout {
		t.Fatalf("unexpected tls ReadTimeout: %v", engine.tlsEngine.server.ReadTimeout)
	}
	if engine.tlsEngine.server.WriteTimeout != defaultWriteTimeout {
		t.Fatalf("unexpected tls WriteTimeout: %v", engine.tlsEngine.server.WriteTimeout)
	}
	if engine.tlsEngine.server.IdleTimeout != defaultIdleTimeout {
		t.Fatalf("unexpected tls IdleTimeout: %v", engine.tlsEngine.server.IdleTimeout)
	}
}

func TestEngineRouteUsesMemoryStore(t *testing.T) {
	engine, err := NewEngine(EngineOptions{
		HTTPListenAddr: "127.0.0.1:80",
	}, nil)
	if err != nil {
		t.Fatalf("new engine: %v", err)
	}
	engine.memory.Put(&BackendBinding{
		ID:       "binding-memory",
		Hostname: "cached.example.com",
		Address:  "127.0.0.1",
		Port:     9000,
		Protocol: RouteKindHTTP,
	})

	first, err := engine.Route(context.Background(), "cached.example.com", "/")
	if err != nil {
		t.Fatalf("first route: %v", err)
	}
	second, err := engine.Route(context.Background(), "cached.example.com", "/")
	if err != nil {
		t.Fatalf("second route: %v", err)
	}

	if !first.Found || !second.Found {
		t.Fatal("expected memory store route to be found")
	}
}

func TestEngineRouteHTTPSUsesHTTPUpstreamForL7Projection(t *testing.T) {
	engine, err := NewEngine(EngineOptions{
		HTTPListenAddr: "127.0.0.1:80",
	}, nil)
	if err != nil {
		t.Fatalf("new engine: %v", err)
	}
	engine.slave = replicaReaderStub{
		reader: routeLookupReaderStub{
			entry: &metadata.DomainEntryProjection{
				ID:          "domain-1",
				Hostname:    "secure.example.com",
				BackendType: metadata.BackendTypeL7HTTPS,
				HTTPRoutes: []metadata.HTTPRouteProjection{{
					ID:       "route-1",
					Path:     "/",
					Priority: 10,
					BackendRef: &metadata.ServiceBackendRef{
						ID:               "backend-1",
						ServiceNamespace: "default",
						ServiceName:      "svc-secure",
						ServicePort:      80,
					},
				}},
			},
		},
	}

	result, err := engine.RouteHTTPS(context.Background(), "secure.example.com", "/")
	if err != nil {
		t.Fatalf("route https: %v", err)
	}
	if !result.Found {
		t.Fatalf("expected route to be found")
	}
	if result.Target.UpstreamURL != "http://svc-secure.default.svc.cluster.local:80" {
		t.Fatalf("unexpected upstream url: %s", result.Target.UpstreamURL)
	}
}

func TestEngineRouteReturnsNotFoundWithoutSources(t *testing.T) {
	engine, err := NewEngine(EngineOptions{
		HTTPListenAddr: "127.0.0.1:80",
	}, nil)
	if err != nil {
		t.Fatalf("new engine: %v", err)
	}

	result, err := engine.Route(context.Background(), "repo.example.com", "/")
	if err != nil {
		t.Fatalf("route: %v", err)
	}
	if result == nil || result.Found {
		t.Fatal("expected route miss without memory, slave, repo, or k8s")
	}
}

func TestEngineRouteUsesK8sPathMatching(t *testing.T) {
	prefix := networkingv1.PathTypePrefix
	className := "luna-edge"
	bridge := NewK8sBridgeWithClient("default", className, fake.NewSimpleClientset())
	bridge.ingresses["default/demo"] = &k8sIngressState{resource: (&networkingv1.Ingress{
		ObjectMeta: metav1ObjectMeta("default", "demo", 1),
		Spec: networkingv1.IngressSpec{
			IngressClassName: &className,
			Rules: []networkingv1.IngressRule{{
				Host: "app.example.com",
				IngressRuleValue: networkingv1.IngressRuleValue{
					HTTP: &networkingv1.HTTPIngressRuleValue{
						Paths: []networkingv1.HTTPIngressPath{
							{
								Path:     "/",
								PathType: &prefix,
								Backend: networkingv1.IngressBackend{
									Service: &networkingv1.IngressServiceBackend{
										Name: "svc-root",
										Port: networkingv1.ServiceBackendPort{Number: 8080},
									},
								},
							},
							{
								Path:     "/admin",
								PathType: &prefix,
								Backend: networkingv1.IngressBackend{
									Service: &networkingv1.IngressServiceBackend{
										Name: "svc-admin",
										Port: networkingv1.ServiceBackendPort{Number: 9090},
									},
								},
							},
						},
					},
				},
			}},
		},
	}).DeepCopy()}
	bridge.rebuildRoutesLocked()

	engine, err := NewEngine(EngineOptions{
		HTTPListenAddr: "127.0.0.1:80",
	}, nil)
	if err != nil {
		t.Fatalf("new engine: %v", err)
	}
	engine.k8sBridge = bridge

	result, err := engine.Route(context.Background(), "app.example.com", "/admin/settings")
	if err != nil {
		t.Fatalf("route admin: %v", err)
	}
	if !result.Found || result.Target.UpstreamURL != "http://svc-admin.default.svc.cluster.local:9090" {
		t.Fatalf("unexpected admin route result: %#v", result)
	}

	result, err = engine.Route(context.Background(), "app.example.com", "/")
	if err != nil {
		t.Fatalf("route root: %v", err)
	}
	if !result.Found || result.Target.UpstreamURL != "http://svc-root.default.svc.cluster.local:8080" {
		t.Fatalf("unexpected root route result: %#v", result)
	}
}
