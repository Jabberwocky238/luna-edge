# engine/master

## 职责
master 控制面核心，负责主库存储、管理接口、复制、K8s 监听、证书申请和广播。

## 架构
- `engine.go` 聚合 master 生命周期。
- `hub.go` 负责变更广播。
- `cert_reconciler.go` 负责周期性证书调和。
- `k8s_bridge/` 监听 Kubernetes 并物化进主库。
- `manage/` 提供 manage API 与带副作用的 repository 包装。
- `acme/` 负责证书申请实现。

## 存在的问题
- 控制面能力较多，模块边界仍在重构中。
- 批量写库、副作用合并和事务语义还需要进一步统一。
