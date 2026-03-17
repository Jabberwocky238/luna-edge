package ingress

import (
	"bufio"
	"context"
	"crypto/tls"
	"io"
	"net"
	"net/http"
	"path/filepath"
	"testing"
)

func TestTLSEngineTLS443OverlapRoutesBySNI(t *testing.T) {
	requireSocketSupport(t)

	certRoot := t.TempDir()
	httpsCA := writeTestCertificate(t, certRoot, "https.example.com")
	termCA := writeTestCertificate(t, certRoot, "term.example.com")
	passCA := writeTestCertificate(t, certRoot, "pass.example.com")

	httpsUpstream := newLocalTestServer(t, http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if got := r.Header.Get("X-Forwarded-Proto"); got != "https" {
			t.Errorf("expected X-Forwarded-Proto=https, got %q", got)
		}
		_, _ = w.Write([]byte("https-ok"))
	}))
	defer httpsUpstream.Close()
	httpsHost, httpsPort := splitHostPort(t, httpsUpstream.Listener.Addr().String())

	termLn, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatalf("listen tcp backend: %v", err)
	}
	defer func() { _ = termLn.Close() }()
	go serveTCPEcho(termLn, "term-ok:")
	termHost, termPort := splitHostPort(t, termLn.Addr().String())

	passBackend := newTLSEchoServer(t, certRoot, "pass.example.com", "pass-ok:")
	defer passBackend.Close()
	passHost, passPort := splitHostPort(t, passBackend.Addr().String())

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
		ID:       "binding-https",
		Hostname: "https.example.com",
		Address:  httpsHost,
		Port:     httpsPort,
		Protocol: RouteKindHTTP,
	})
	engine.memory.Put(&BackendBinding{
		ID:       "binding-term",
		Hostname: "term.example.com",
		Address:  termHost,
		Port:     termPort,
		Protocol: RouteKindTLSTerminate,
	})
	engine.memory.Put(&BackendBinding{
		ID:       "binding-pass",
		Hostname: "pass.example.com",
		Address:  passHost,
		Port:     passPort,
		Protocol: RouteKindTLSPassthrough,
	})
	if err := engine.Listen(); err != nil {
		t.Fatalf("listen: %v", err)
	}
	defer func() { _ = engine.Stop(context.Background()) }()

	waitForTLSServer(t, engine.opts.TLSListenAddr, "https.example.com", httpsCA)

	httpsClient := &http.Client{
		Transport: &http.Transport{
			TLSClientConfig: &tls.Config{
				ServerName: "https.example.com",
				RootCAs:    httpsCA,
			},
		},
	}
	resp, err := httpsClient.Get("https://" + engine.opts.TLSListenAddr + "/")
	if err != nil {
		t.Fatalf("https request: %v", err)
	}
	body, err := io.ReadAll(resp.Body)
	_ = resp.Body.Close()
	if err != nil {
		t.Fatalf("read https body: %v", err)
	}
	if string(body) != "https-ok" {
		t.Fatalf("unexpected https body: %q", string(body))
	}

	termConn, err := tls.Dial("tcp", engine.opts.TLSListenAddr, &tls.Config{
		ServerName: "term.example.com",
		RootCAs:    termCA,
	})
	if err != nil {
		t.Fatalf("dial tls termination path: %v", err)
	}
	if _, err := termConn.Write([]byte("ping\n")); err != nil {
		t.Fatalf("write term data: %v", err)
	}
	termReply, err := bufio.NewReader(termConn).ReadString('\n')
	_ = termConn.Close()
	if err != nil {
		t.Fatalf("read term data: %v", err)
	}
	if termReply != "term-ok:ping\n" {
		t.Fatalf("unexpected tls termination reply: %q", termReply)
	}

	passConn, err := tls.Dial("tcp", engine.opts.TLSListenAddr, &tls.Config{
		ServerName: "pass.example.com",
		RootCAs:    passCA,
	})
	if err != nil {
		t.Fatalf("dial tls passthrough path: %v", err)
	}
	if _, err := passConn.Write([]byte("pong\n")); err != nil {
		t.Fatalf("write pass data: %v", err)
	}
	passReply, err := bufio.NewReader(passConn).ReadString('\n')
	_ = passConn.Close()
	if err != nil {
		t.Fatalf("read pass data: %v", err)
	}
	if passReply != "pass-ok:pong\n" {
		t.Fatalf("unexpected tls passthrough reply: %q", passReply)
	}
}

func serveTCPEcho(ln net.Listener, prefix string) {
	for {
		conn, err := ln.Accept()
		if err != nil {
			return
		}
		go func(c net.Conn) {
			defer func() { _ = c.Close() }()
			line, err := bufio.NewReader(c).ReadString('\n')
			if err != nil {
				return
			}
			_, _ = io.WriteString(c, prefix+line)
		}(conn)
	}
}

type tlsEchoServer struct {
	ln net.Listener
}

func newTLSEchoServer(t *testing.T, certRoot, hostname, prefix string) *tlsEchoServer {
	t.Helper()
	certDir := filepath.Join(certRoot, certificateDirectoryName(hostname))
	cert, err := tls.LoadX509KeyPair(filepath.Join(certDir, "tls.crt"), filepath.Join(certDir, "tls.key"))
	if err != nil {
		t.Fatalf("load key pair: %v", err)
	}
	ln, err := tls.Listen("tcp", "127.0.0.1:0", &tls.Config{
		MinVersion:   tls.VersionTLS12,
		Certificates: []tls.Certificate{cert},
	})
	if err != nil {
		t.Fatalf("listen tls echo: %v", err)
	}
	go func() {
		for {
			conn, acceptErr := ln.Accept()
			if acceptErr != nil {
				return
			}
			go func(c net.Conn) {
				defer func() { _ = c.Close() }()
				line, readErr := bufio.NewReader(c).ReadString('\n')
				if readErr != nil {
					return
				}
				_, _ = io.WriteString(c, prefix+line)
			}(conn)
		}
	}()
	return &tlsEchoServer{ln: ln}
}

func (s *tlsEchoServer) Addr() net.Addr { return s.ln.Addr() }
func (s *tlsEchoServer) Close() error   { return s.ln.Close() }

func TestTLSEngineWildcardCertificateUsedForHTTPSListener(t *testing.T) {
	requireSocketSupport(t)

	certRoot := t.TempDir()
	rootCA := writeTestCertificate(t, certRoot, "*.nginx.app238.com")
	upstream := newLocalTestServer(t, http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		_, _ = io.WriteString(w, "wildcard-ok")
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
		ID:       "wildcard-binding",
		Hostname: "aaa.nginx.app238.com",
		Address:  host,
		Port:     port,
		Protocol: RouteKindHTTP,
	})
	if err := engine.Listen(); err != nil {
		t.Fatalf("listen: %v", err)
	}
	defer func() { _ = engine.Stop(context.Background()) }()

	waitForTLSServer(t, engine.opts.TLSListenAddr, "aaa.nginx.app238.com", rootCA)
	client := &http.Client{
		Transport: &http.Transport{
			TLSClientConfig: &tls.Config{
				ServerName: "aaa.nginx.app238.com",
				RootCAs:    rootCA,
			},
		},
	}
	resp, err := client.Get("https://" + engine.opts.TLSListenAddr + "/")
	if err != nil {
		t.Fatalf("wildcard https request: %v", err)
	}
	body, err := io.ReadAll(resp.Body)
	_ = resp.Body.Close()
	if err != nil {
		t.Fatalf("read wildcard body: %v", err)
	}
	if string(body) != "wildcard-ok" {
		t.Fatalf("unexpected wildcard body: %q", string(body))
	}
}
