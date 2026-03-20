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

// EngineOptions 定义 ingress 引擎的初始化参数。
type EngineOptions struct {
	HTTPListenAddr       string
	TLSListenAddr        string
	K8sEnabled           bool
	K8sNamespace         string
	K8sIngressClass      string
	LRUSize              int
	MasterHTTP01ProxyURL string
}

// ProxyTarget 表示一个可转发的上游目标。
type ProxyTarget struct {
	Hostname    string
	Port        uint32
	UpstreamURL string
	Protocol    string
	PathPrefix  string
}

// RouteResult 表示一次 ingress 路由匹配结果。
type RouteResult struct {
	Found  bool
	Target ProxyTarget
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
