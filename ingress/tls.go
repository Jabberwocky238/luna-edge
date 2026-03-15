package ingress

import (
	"context"
	"crypto/tls"
	"fmt"
	"net"
	"net/http"
)

// TLSEngine 负责 TLS 终止、动态加载证书、识别 SNI，并把解密后的请求转给 HTTP 处理层。
type TLSEngine struct {
	resolver TLSCertResolver
	handler  http.Handler
	server   *http.Server
	listener net.Listener
}

// NewTLSEngine 创建一个 TLS 执行引擎。
func NewTLSEngine(resolver TLSCertResolver, server *http.Server) (*TLSEngine, error) {
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
	}, nil
}

// Listen 启动 TLS 监听。
func (e *TLSEngine) Listen() error {
	if e.server != nil && e.server.Addr == "" {
		return fmt.Errorf("tls server is missing listen address")
	}
	if e.listener != nil {
		return fmt.Errorf("tls listener already started")
	}

	tlsConfig := &tls.Config{
		MinVersion: tls.VersionTLS12,
		GetCertificate: func(hello *tls.ClientHelloInfo) (*tls.Certificate, error) {
			serverName := normalizeHost(hello.ServerName)
			if serverName == "" {
				return nil, fmt.Errorf("missing sni")
			}
			return e.resolver.Load(serverName)
		},
	}

	e.server.TLSConfig = tlsConfig
	e.server.Handler = http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.TLS != nil {
			sni := normalizeHost(r.TLS.ServerName)
			if sni != "" {
				r.Header.Set("X-Forwarded-Host", r.Host)
				r.Host = sni
				r.Header.Set("Host", sni)
				r.Header.Set("X-Forwarded-Proto", "https")
			}
		}
		e.handler.ServeHTTP(w, r)
	})

	lis, err := net.Listen("tcp", e.server.Addr)
	if err != nil {
		return err
	}
	e.listener = lis

	go func() {
		_ = e.server.Serve(tls.NewListener(lis, tlsConfig))
	}()

	return nil
}

// Stop 停止 TLS 监听。
func (e *TLSEngine) Stop(ctx context.Context) error {
	if e.server == nil {
		return nil
	}
	return e.server.Shutdown(ctx)
}
