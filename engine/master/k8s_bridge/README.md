# engine/master/k8s_bridge

## 职责
master 侧 Kubernetes bridge，监听 DNS、Ingress、Gateway 资源并物化到主库。

## 架构
- `bridge.go` 聚合 DNS/Ingress/Gateway 三类桥。
- `dns.go` 处理 DNS CRD 到 `DNSRecord` 的物化。
- `ingress.go` 处理 Ingress 到 `DomainEndpoint/HTTPRoute/ServiceBackendRef` 的物化。
- `gateway.go`、`gateway_httproute.go`、`gateway_tlsroute.go` 处理 Gateway API。
- 通过 `manage.Wrapper.Batch` 合并批量写库后的副作用。

## 存在的问题
- 物化逻辑仍偏重，重复模式较多。
- 目前批处理先解决副作用合并，事务语义还未完全引入。
