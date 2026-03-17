# ingress

## 职责

HTTP/TLS 数据面运行时，负责从 slave 本地缓存解析路由并监听 `:80` / `:443`。

## 当前语义

- `http`: 明文 HTTP
- `https`: 入口终止 TLS，再转 HTTP
- `grpc`: 入口终止 TLS/HTTP2 后按 gRPC 语义转发
- `tls-route`: 入口终止 TLS，再把明文 TCP 转给后端
- `tls-passthrough`: 完全不终止 TLS，原始 TLS 流直接透传

## 443 复用

同一个 `:443` 可以按 SNI 分流三类流量：

1. `tls-passthrough`
2. `tls-route`
3. `https`

当前 `engine_tls.go` 的优先级就是这个顺序。

## 架构

- `engine.go` 聚合 HTTP/TLS 运行时
- `engine_http.go` 处理明文 HTTP
- `engine_tls.go` 处理 TLS terminate / passthrough / terminate-to-tcp 分流
- `memory_store.go` 保存运行时路由视图
- `resolver*.go` 从本地磁盘证书目录加载证书

## 当前边界

- master 负责监听 Kubernetes 并写主库
- slave 通过复制把 `DomainEntryProjection` 物化到本地
- ingress 不再承担主控制面的 K8s 监听职责

## 存在的问题

- 历史上留下的 `bridge` 命名仍然容易让人误解为控制面
- L4/L7 混合在一个运行时里，后续仍可继续压缩路径
