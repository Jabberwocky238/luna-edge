# Ingress

`ingress` 目录负责统一处理以下几类入口来源：

- 传统 Kubernetes `Ingress`
- Gateway API `HTTPRoute`
- Gateway API `GRPCRoute`
- Gateway API `TLSRoute`
- Gateway API `TCPRoute`
- Gateway API `UDPRoute`

核心目标不是只代理 HTTP，而是把不同入口协议物化成统一的后端解析结果，再由不同 engine 消费。

## Route Kind

当前系统内部使用以下路由类型：

- `http`
- `https`
- `grpc`
- `tls-route`
- `tls-passthrough`
- `tcp`
- `udp`

它们的语义是：

- `http`
  明文 HTTP，直接按 HTTP 转发。
- `https`
  在入口侧终止 TLS，然后按 HTTP 转发。
- `grpc`
  作为独立类型保留，当前仍基于 host/path 维度匹配。
- `tls-route`
  在入口侧终止 TLS，然后将解密后的明文流按 TCP 转发。
  这个语义适合 `trojan` 一类“先 TLS，再自定义 TCP 协议”的场景。
- `tls-passthrough`
  不在入口侧终止 TLS，原始 TLS 流直接透传给后端。
  这个语义适合后端自己处理 TLS 的场景，例如 `vless over tls`。
- `tcp`
  普通四层 TCP 转发。
- `udp`
  普通四层 UDP 转发。

## 443 Multiplex

同一个 `:443` 监听口当前可以承载多类 TLS 流量，前提是它们能通过 SNI 区分：

- 普通网站 HTTPS
  走 `https`
  流程：TLS terminate -> HTTP proxy
- Trojan
  走 `tls-route`
  流程：TLS terminate -> TCP relay
- VLESS over TLS
  走 `tls-passthrough`
  流程：raw TLS passthrough

注意：

- 入口当前只基于 TLS `ClientHello` / `SNI` 做分流
- 入口不会解析 `trojan` 或 `vless` 协议本身
- 是否支持这些协议，取决于后端服务是否能处理解密后或透传后的流量

## Ingress Compatibility

传统 Kubernetes `Ingress` 只兼容成卸载后的 Web 入口，不参与 `tls-route` 语义：

- `web` -> `http`
- `websecure` -> `https`

也就是说：

- Ingress 不再兼容成 `tls-route`
- Ingress 的 `websecure` 语义固定为“终止 TLS 后按 HTTP 转发”

## Gateway API Compatibility

Gateway API 的 listener / route 映射规则如下：

- listener `web` -> `http`
- listener `websecure` -> `https`
- `HTTPRoute` -> 根据挂载 listener 进入 `http` 或 `https`
- `GRPCRoute` -> `grpc`
- `TLSRoute` -> `tls-route`
- `TCPRoute` -> `tcp`
- `UDPRoute` -> `udp`
- `TLS listener + mode=Passthrough` -> `tls-passthrough`

这意味着：

- `websecure` 不再映射为 `tls-route`
- 原生 `TLSRoute` 与 `tls-passthrough` 是两种不同语义
- `TLSRoute` 表示入口终止 TLS 后转 TCP
- `tls-passthrough` 表示入口完全不终止 TLS

## Engine Split

当前 engine 侧的大致职责如下：

- `engine_http.go`
  处理明文 HTTP 入口
- `engine_tls.go`
  处理 TLS 入口，并在 `https` / `tls-route` / `tls-passthrough` 之间分流
- `k8s_bridge_ingress.go`
  监听并物化传统 `Ingress`
- `k8s_bridge_gateway.go`
  监听并物化 Gateway API 资源

`engine_tls.go` 的当前分流优先级：

1. 命中 `tls-passthrough`
2. 命中 `tls-route`
3. 命中 `https`
4. 未命中时回退到本地 TLS terminate + HTTP 处理
