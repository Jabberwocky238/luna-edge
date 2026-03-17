package ingress

import (
	"bufio"
	"context"
	"crypto/tls"
	"fmt"
	"io"
	"net"
	"net/http"
	"sync"
	"time"
)

// TLSEngine 负责 TLS 入口分流：
// - 命中 TLS passthrough: 原样代理 TLS 流
// - 命中 TLSRoute: 本地终止 TLS，再把明文流按 TCP 转发
// - 命中 HTTPS: 本地终止 TLS，再交给 HTTP 处理层
// - 未命中: 回退到本地 TLS 终止 + HTTP 处理层
type TLSEngine struct {
	resolver TLSCertResolver
	handler  http.Handler
	server   *http.Server
	listener net.Listener
	// 存储器
	bridge *K8sBridge
	memory *memoryStore
}

// NewTLSEngine 创建一个 TLS 执行引擎。
func NewTLSEngine(resolver TLSCertResolver, server *http.Server, bridge *K8sBridge) (*TLSEngine, error) {
	if resolver == nil {
		return nil, fmt.Errorf("tls certificate resolver is required")
	}
	if server == nil {
		return nil, fmt.Errorf("http server is required")
	}

	return &TLSEngine{
		resolver: resolver,
		handler:  server.Handler,
		server:   server,
		bridge:   bridge,
	}, nil
}

func (e *TLSEngine) SetK8sBridge(bridge *K8sBridge) {
	e.bridge = bridge
}

func (e *TLSEngine) SetMemoryStore(memory *memoryStore) {
	e.memory = memory
}

// Listen 启动 TLS 监听。
func (e *TLSEngine) Listen() error {
	if e.server != nil && e.server.Addr == "" {
		return fmt.Errorf("tls server is missing listen address")
	}
	if e.listener != nil {
		return fmt.Errorf("tls listener already started")
	}

	lis, err := net.Listen("tcp", e.server.Addr)
	if err != nil {
		return err
	}
	e.listener = lis

	go e.serveLoop()
	return nil
}

func (e *TLSEngine) serveLoop() {
	for {
		conn, err := e.listener.Accept()
		if err != nil {
			return
		}
		go e.serveConn(conn)
	}
}

func (e *TLSEngine) serveConn(conn net.Conn) {
	buffered := newPeekConn(conn)
	serverName, err := buffered.PeekServerName()
	if err != nil {
		_ = conn.Close()
		return
	}
	serverName = normalizeHost(serverName)
	if serverName == "" {
		_ = conn.Close()
		return
	}

	if passthrough, ok := e.resolveTLSPassthroughBackend(serverName); ok {
		_ = proxyStream(buffered, passthrough.Binding.Address, passthrough.Binding.Port)
		return
	}

	tlsConn := tls.Server(buffered, &tls.Config{
		MinVersion: tls.VersionTLS12,
		GetCertificate: func(hello *tls.ClientHelloInfo) (*tls.Certificate, error) {
			name := normalizeHost(hello.ServerName)
			if name == "" {
				name = serverName
			}
			if name == "" {
				return nil, fmt.Errorf("missing sni")
			}
			return e.resolver.Load(name)
		},
	})

	if err := tlsConn.Handshake(); err != nil {
		_ = tlsConn.Close()
		return
	}

	if tlsRoute, ok := e.resolveTLSRouteBackend(serverName); ok {
		_ = proxyStream(tlsConn, tlsRoute.Binding.Address, tlsRoute.Binding.Port)
		return
	}

	srv := &http.Server{
		Handler: http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			r.Header.Set("X-Forwarded-Host", r.Host)
			r.Host = serverName
			r.Header.Set("Host", serverName)
			r.Header.Set("X-Forwarded-Proto", "https")
			e.handler.ServeHTTP(w, r)
		}),
		ReadHeaderTimeout: e.server.ReadHeaderTimeout,
		ReadTimeout:       e.server.ReadTimeout,
		WriteTimeout:      e.server.WriteTimeout,
		IdleTimeout:       e.server.IdleTimeout,
	}
	_ = srv.Serve(&singleConnListener{conn: tlsConn})
}

func (e *TLSEngine) resolveTLSPassthroughBackend(serverName string) (*K8sResolvedBackend, bool) {
	if e.bridge == nil {
		return e.resolveMemoryBackend(serverName, RouteKindTLSPassthrough)
	}
	backend, ok := e.bridge.ResolveTLSPassthrough(serverName)
	if ok {
		return backend, true
	}
	return e.resolveMemoryBackend(serverName, RouteKindTLSPassthrough)
}

func (e *TLSEngine) resolveTLSRouteBackend(serverName string) (*K8sResolvedBackend, bool) {
	if e.bridge != nil {
		backend, ok := e.bridge.ResolveTLS(serverName)
		if ok {
			return backend, true
		}
	}
	return e.resolveMemoryBackend(serverName, RouteKindTLSTerminate)
}

func (e *TLSEngine) resolveMemoryBackend(serverName string, kind RouteKind) (*K8sResolvedBackend, bool) {
	if e.memory == nil {
		return nil, false
	}
	binding, ok := e.memory.GetByProtocol(serverName, "/", kind)
	if !ok || binding == nil {
		return nil, false
	}
	return &K8sResolvedBackend{
		Kind:     kind,
		Hostname: normalizeHost(serverName),
		Binding:  binding,
	}, true
}

// Stop 停止 TLS 监听。
func (e *TLSEngine) Stop(ctx context.Context) error {
	if e.listener != nil {
		_ = e.listener.Close()
	}
	if e.server == nil {
		return nil
	}
	return e.server.Shutdown(ctx)
}

type singleConnListener struct {
	conn   net.Conn
	closed bool
	mu     sync.Mutex
}

func (l *singleConnListener) Accept() (net.Conn, error) {
	l.mu.Lock()
	defer l.mu.Unlock()
	if l.closed || l.conn == nil {
		return nil, io.EOF
	}
	conn := l.conn
	l.conn = nil
	return conn, nil
}

func (l *singleConnListener) Close() error { return nil }
func (l *singleConnListener) Addr() net.Addr {
	if l.conn != nil {
		return l.conn.LocalAddr()
	}
	return dummyAddr("tls")
}

type peekConn struct {
	net.Conn
	reader *bufio.Reader
}

func newPeekConn(conn net.Conn) *peekConn {
	return &peekConn{
		Conn:   conn,
		reader: bufio.NewReader(conn),
	}
}

func (c *peekConn) Read(p []byte) (int, error) {
	return c.reader.Read(p)
}

func (c *peekConn) PeekServerName() (string, error) {
	if err := c.Conn.SetReadDeadline(time.Now().Add(2 * time.Second)); err != nil {
		return "", err
	}
	defer func() { _ = c.Conn.SetReadDeadline(time.Time{}) }()

	header, err := c.reader.Peek(5)
	if err != nil {
		return "", err
	}
	if len(header) < 5 || header[0] != 22 {
		return "", fmt.Errorf("not a tls handshake")
	}
	recordLen := int(header[3])<<8 | int(header[4])
	frame, err := c.reader.Peek(5 + recordLen)
	if err != nil {
		return "", err
	}
	return parseTLSClientHelloServerName(frame)
}

func parseTLSClientHelloServerName(frame []byte) (string, error) {
	if len(frame) < 43 || frame[0] != 22 {
		return "", fmt.Errorf("invalid tls frame")
	}
	pos := 5
	if frame[pos] != 1 {
		return "", fmt.Errorf("not client hello")
	}
	pos += 4
	pos += 2
	pos += 32
	if pos >= len(frame) {
		return "", io.ErrUnexpectedEOF
	}
	sessionIDLen := int(frame[pos])
	pos++
	pos += sessionIDLen
	if pos+2 > len(frame) {
		return "", io.ErrUnexpectedEOF
	}
	cipherLen := int(frame[pos])<<8 | int(frame[pos+1])
	pos += 2 + cipherLen
	if pos >= len(frame) {
		return "", io.ErrUnexpectedEOF
	}
	compressionLen := int(frame[pos])
	pos++
	pos += compressionLen
	if pos+2 > len(frame) {
		return "", io.ErrUnexpectedEOF
	}
	extLen := int(frame[pos])<<8 | int(frame[pos+1])
	pos += 2
	end := pos + extLen
	if end > len(frame) {
		return "", io.ErrUnexpectedEOF
	}
	for pos+4 <= end {
		extType := int(frame[pos])<<8 | int(frame[pos+1])
		extSize := int(frame[pos+2])<<8 | int(frame[pos+3])
		pos += 4
		if pos+extSize > end {
			return "", io.ErrUnexpectedEOF
		}
		if extType == 0 {
			if pos+2 > end {
				return "", io.ErrUnexpectedEOF
			}
			listLen := int(frame[pos])<<8 | int(frame[pos+1])
			i := pos + 2
			listEnd := i + listLen
			for i+3 <= listEnd && i+3 <= len(frame) {
				nameType := frame[i]
				nameLen := int(frame[i+1])<<8 | int(frame[i+2])
				i += 3
				if i+nameLen > listEnd || i+nameLen > len(frame) {
					return "", io.ErrUnexpectedEOF
				}
				if nameType == 0 {
					return string(frame[i : i+nameLen]), nil
				}
				i += nameLen
			}
		}
		pos += extSize
	}
	return "", fmt.Errorf("sni not found")
}

func proxyStream(src net.Conn, host string, port uint32) error {
	target, err := net.Dial("tcp", net.JoinHostPort(host, fmt.Sprintf("%d", port)))
	if err != nil {
		_ = src.Close()
		return err
	}

	copyConn := func(dst io.WriteCloser, reader io.ReadCloser) {
		_, _ = io.Copy(dst, reader)
		_ = dst.Close()
		_ = reader.Close()
	}

	go copyConn(target, src)
	go copyConn(src, target)
	return nil
}

type dummyAddr string

func (d dummyAddr) Network() string { return string(d) }
func (d dummyAddr) String() string  { return string(d) }
