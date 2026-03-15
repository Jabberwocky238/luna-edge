package ingress

import (
	"context"
	"fmt"
	"net"
	"net/http"
)

// HTTPEngine 负责纯 HTTP 监听，并把请求转发给底层 serveHTTP。
type HTTPEngine struct {
	server     *http.Server
	listenAddr string
	listener   net.Listener
}

// NewHTTPEngine 创建一个 HTTP 执行引擎。
//
// listenAddr 是 HTTP 监听地址。
// server 是由主 Engine 初始化并持有的共享 HTTP server。
func NewHTTPEngine(listenAddr string, server *http.Server) (*HTTPEngine, error) {
	if listenAddr == "" {
		return nil, fmt.Errorf("http listen address is required")
	}
	if server == nil {
		return nil, fmt.Errorf("http server is required")
	}

	return &HTTPEngine{
		server:     server,
		listenAddr: listenAddr,
	}, nil
}

// Listen 启动 HTTP 监听。
func (e *HTTPEngine) Listen() error {
	if e.server == nil {
		return fmt.Errorf("http server is not configured")
	}
	if e.listener != nil {
		return fmt.Errorf("http listener already started")
	}
	lis, err := net.Listen("tcp", e.listenAddr)
	if err != nil {
		return err
	}
	e.listener = lis
	go func() {
		_ = e.server.Serve(lis)
	}()
	return nil
}

// Stop 停止 HTTP 监听。
func (e *HTTPEngine) Stop(ctx context.Context) error {
	if e.server == nil {
		return nil
	}
	return e.server.Shutdown(ctx)
}

// NewHTTPHandler 返回经过中间件包装的 HTTP 处理器。
func (e *Engine) NewHTTPHandler() http.Handler {
	var handler http.Handler = http.HandlerFunc(e.serveHTTP)
	for i := len(e.middlewares) - 1; i >= 0; i-- {
		handler = e.middlewares[i](handler)
	}
	return handler
}
