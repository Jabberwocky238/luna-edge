package ingress

import (
	"context"
	"fmt"
	"log"
	"net"
	"net/http"
	"net/http/httputil"
	"net/url"
	"strings"
)

// HTTPEngine 负责纯 HTTP 监听，并把请求转发给底层 serveHTTP。
type HTTPEngine struct {
	ctx        context.Context
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
		ctx:        context.Background(),
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
func (e *HTTPEngine) Stop() error {
	if e.server == nil {
		return nil
	}
	return e.server.Shutdown(e.ctx)
}

// NewHTTPHandler 返回经过中间件包装的 HTTP 处理器。
func (e *Engine) NewHTTPHandler() http.Handler {
	var handler http.Handler = http.HandlerFunc(e.serveHTTP)
	for i := len(e.middlewares) - 1; i >= 0; i-- {
		handler = e.middlewares[i](handler)
	}
	return handler
}

func (e *Engine) serveHTTP(w http.ResponseWriter, r *http.Request) {
	logIngressHTTPRequest(r)

	if strings.HasPrefix(r.URL.Path, acmeHTTP01Prefix) && strings.TrimSpace(e.opts.MasterHTTP01ProxyURL) != "" {
		e.serveACMEHTTP01Proxy(w, r)
		return
	}

	var (
		result *RouteResult
		err    error
	)
	if strings.EqualFold(r.Header.Get("X-Forwarded-Proto"), "https") {
		result, err = e.RouteHTTPS(r.Context(), r.Host, r.URL.Path)
	} else {
		result, err = e.Route(r.Context(), r.Host, r.URL.Path)
	}
	if err != nil {
		http.Error(w, err.Error(), http.StatusInternalServerError)
		return
	}
	if result == nil || !result.Found {
		http.NotFound(w, r)
		return
	}

	targetURL, err := url.Parse(result.Target.UpstreamURL)
	if err != nil {
		http.Error(w, "invalid upstream url", http.StatusInternalServerError)
		return
	}

	proxy := httputil.NewSingleHostReverseProxy(targetURL)
	originalDirector := proxy.Director
	proxy.Director = func(req *http.Request) {
		originalDirector(req)
		req.URL.Path, req.URL.RawPath = rewriteUpstreamPath(result.Target.PathPrefix, r.URL.Path, r.URL.RawPath)
		req.URL.RawQuery = r.URL.RawQuery
		req.Host = targetURL.Host
	}
	proxy.ErrorHandler = func(rw http.ResponseWriter, req *http.Request, proxyErr error) {
		http.Error(rw, proxyErr.Error(), http.StatusBadGateway)
	}
	proxy.ServeHTTP(w, r)
}

func rewriteUpstreamPath(prefix, requestPath, rawPath string) (string, string) {
	prefix = normalizeProxyPathPrefix(prefix)
	if requestPath == "" {
		requestPath = "/"
	}
	trimmedPath := trimMatchedPrefix(prefix, requestPath)
	trimmedRawPath := rawPath
	if rawPath != "" {
		trimmedRawPath = trimMatchedPrefix(prefix, rawPath)
	}
	return trimmedPath, trimmedRawPath
}

func trimMatchedPrefix(prefix, path string) string {
	if path == "" {
		return "/"
	}
	if prefix == "/" || prefix == "" {
		return path
	}
	trimmed := strings.TrimPrefix(path, prefix)
	if trimmed == "" {
		return "/"
	}
	if !strings.HasPrefix(trimmed, "/") {
		return "/" + trimmed
	}
	return trimmed
}

func logIngressHTTPRequest(r *http.Request) {
	if r == nil {
		return
	}
	var sni string
	if r.Header != nil {
		sni = strings.TrimSpace(r.Header.Get("X-Luna-SNI"))
	}
	if sni == "" && r.TLS != nil {
		sni = normalizeHost(r.TLS.ServerName)
	}
	clientIP := ingressClientIP(r)
	requestURI := ""
	if r.URL != nil {
		requestURI = r.URL.RequestURI()
	}
	log.Printf("[INGRESS] http request method=%s path=%q host=%q sni=%q client_ip=%q remote_addr=%q", r.Method, requestURI, r.Host, sni, clientIP, r.RemoteAddr)
}

func ingressClientIP(r *http.Request) string {
	if r == nil {
		return ""
	}
	if forwardedFor := strings.TrimSpace(r.Header.Get("X-Forwarded-For")); forwardedFor != "" {
		parts := strings.Split(forwardedFor, ",")
		if len(parts) > 0 {
			return strings.TrimSpace(parts[0])
		}
	}
	if realIP := strings.TrimSpace(r.Header.Get("X-Real-IP")); realIP != "" {
		return realIP
	}
	host, _, err := net.SplitHostPort(strings.TrimSpace(r.RemoteAddr))
	if err == nil {
		return host
	}
	return strings.TrimSpace(r.RemoteAddr)
}

func (e *Engine) serveACMEHTTP01Proxy(w http.ResponseWriter, r *http.Request) {
	targetURL, err := url.Parse(strings.TrimRight(e.opts.MasterHTTP01ProxyURL, "/"))
	if err != nil {
		http.Error(w, "invalid acme http01 proxy url", http.StatusInternalServerError)
		return
	}
	proxy := httputil.NewSingleHostReverseProxy(targetURL)
	originalDirector := proxy.Director
	proxy.Director = func(req *http.Request) {
		originalDirector(req)
		req.URL.Path = r.URL.Path
		req.URL.RawPath = r.URL.RawPath
		req.URL.RawQuery = r.URL.RawQuery
		req.Host = targetURL.Host
	}
	proxy.ErrorHandler = func(rw http.ResponseWriter, req *http.Request, proxyErr error) {
		http.Error(rw, proxyErr.Error(), http.StatusBadGateway)
	}
	proxy.ServeHTTP(w, r)
}
