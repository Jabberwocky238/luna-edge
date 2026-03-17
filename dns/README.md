# dns

## 职责
DNS 数据面运行时，负责根据本地缓存回答 DNS 查询，并兼容部分 Kubernetes 监听逻辑。

## 架构
- `engine.go` 负责 DNS 服务生命周期。
- `cache.go` 管理内存缓存。
- `resolve.go`、`forward.go` 负责本地解析和上游转发。
- `dns-add.go`、`dns-del.go`、`dns-mod.go` 处理记录变更。
- `k8s_bridge.go` 是 DNS 侧 Kubernetes 适配。

## 存在的问题
- master/slave 对 K8s bridge 的职责边界历史上混乱，仍有继续收敛空间。
- 解析、缓存和桥接代码耦合偏高。
