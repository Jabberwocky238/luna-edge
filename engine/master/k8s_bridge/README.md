# engine/master/k8s_bridge

## 职责

master 侧 Kubernetes bridge，监听 DNS、Ingress、Gateway 资源，把声明物化到 master 主库。

## 架构

- `bridge.go`: 聚合 DNS / Ingress / Gateway 三类 bridge
- `dns.go`: 处理 DNS CRD 到 `DNSRecord`
- `ingress.go`: 处理 Ingress 到 `DomainEndpoint` / `HTTPRoute` / `ServiceBackendRef`
- `gateway.go` + `gateway_*.go`: 处理 Gateway API

## 当前行为顺序

1. 根据 hostname 做增量重建，不做无意义全量重建
2. 通过 `manage.Wrapper.Batch` 写主库
3. 如果需要证书，触发 cert reconcile / notify
4. 由 manage 层发布 replication changelog

## 当前边界

- 这是 master 的 bridge，不是 slave 的 bridge
- master bridge 关注“监听并写库 + 副作用 + 广播”
- slave 运行时只关注“本地立即生效”

## 存在的问题

- Ingress / Gateway 物化逻辑还有重复代码
- 事务语义虽然已有批处理入口，但还没完全统一到底层 repository
