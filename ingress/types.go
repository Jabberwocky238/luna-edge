// Package ingress 定义基础入口代理能力。
package ingress

import (
	"context"
	"net/http"

	"github.com/jabberwocky238/luna-edge/repository/metadata"
)

type RouteKind string

const (
	RouteKindHTTP           RouteKind = "http"
	RouteKindHTTPS          RouteKind = "https"
	RouteKindGRPC           RouteKind = "grpc"
	RouteKindTLSTerminate   RouteKind = "tls-terminate"
	RouteKindTLSPassthrough RouteKind = "tls-passthrough"
	RouteKindTCP            RouteKind = "tcp"
	RouteKindUDP            RouteKind = "udp"
)

type BackendBinding struct {
	ID            string
	DomainID      string
	Hostname      string
	ServiceID     string
	Namespace     string
	Name          string
	Address       string
	Port          uint32
	Protocol      RouteKind
	RouteVersion  uint64
	Path          string
	Priority      int32
	BackendJSON   string
	BackendRef    *metadata.ServiceBackendRef
	DomainEntryID string
}

// EngineOptions 定义主 ingress 引擎的初始化参数。
type EngineOptions struct {
	// HTTPListenAddr 是 HTTP 监听地址，例如 :80。
	HTTPListenAddr string
	// TLSListenAddr 是 HTTPS/TLS 监听地址，例如 :443。
	TLSListenAddr string
	// K8sEnabled 表示是否启用 Kubernetes Ingress bridge。
	K8sEnabled bool
	// K8sNamespace 是需要监听的命名空间；为空时自动从环境推断。
	K8sNamespace string
	// K8sIngressClass 是当前 bridge 负责的 IngressClass 名称。
	K8sIngressClass string
	// LRUSize 是 ingress 运行时 LRU 缓冲大小；<=0 时默认 4096。
	LRUSize int
}

// ProxyTarget 表示一个可转发的上游目标。
type ProxyTarget struct {
	// Hostname 是当前请求匹配的域名。
	Hostname string
	// Port 是当前入口监听端口。
	Port uint32
	// UpstreamURL 是反向代理目标地址。
	UpstreamURL string
	// Protocol 是入口协议，例如 http。
	Protocol string
}

// RouteResult 表示一次 ingress 路由匹配结果。
type RouteResult struct {
	// Found 表示是否匹配到可用上游。
	Found bool
	// Target 是匹配到的上游目标。
	Target ProxyTarget
	// StaticResponse 表示直接由 ingress 返回静态内容。
	StaticResponse *StaticResponse
}

// StaticResponse 表示一个直接回写给客户端的响应。
type StaticResponse struct {
	StatusCode  int
	ContentType string
	Body        []byte
}

// K8sResolvedBackend 表示 bridge 对外暴露的一次 Kubernetes 路由解析结果。
type K8sResolvedBackend struct {
	Kind     RouteKind
	Hostname string
	Port     uint32
	Binding  *BackendBinding
	Route    *ResolvedRoute
}

// ResolvedRoute 表示 bridge 内部物化出的路由结果。
type ResolvedRoute struct {
	DomainID     string
	Hostname     string
	RouteVersion uint64
	Protocol     RouteKind
	RouteJSON    string
	BindingID    string
}

// Middleware 定义基础 ingress 中间件能力。
type Middleware func(http.Handler) http.Handler

// CertificatePaths 描述某个 hostname 当前证书在本地的路径。
type CertificatePaths struct {
	CurrentDir  string
	Certificate string
	PrivateKey  string
}

// ProjectionReader 定义 ingress 所需的最小仓储读取能力。
type ProjectionReader interface {
	GetDomainEntryProjectionByDomain(ctx context.Context, domain string) (*metadata.DomainEntryProjection, error)
}
