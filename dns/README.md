# dns

## 职责

DNS 数据面运行时，负责根据 slave 本地缓存回答 DNS 查询。

## 架构

- `engine.go` 管理 DNS 生命周期、转发器、GeoIP 和可选的本地 K8s DNS bridge
- `resolve.go` 负责从内存记录集中解析查询
- `forward.go` 负责未命中时向上游转发
- `cache.go` 管理内存问题缓存
- `dns-add.go`、`dns-mod.go`、`dns-del.go` 提供记录操作辅助逻辑

## 当前边界

- 主控制面 DNS 物化由 `engine/master/k8s_bridge/dns.go` 负责
- slave 侧 `dns` 目录只负责运行时解析
- 这里的 `k8s_bridge.go` 只是本地 DNS 运行时可选兼容层，不是主架构中心

## 存在的问题

- 历史上 control plane 和 runtime 的 K8s 职责混在一起，文档和命名仍有旧痕迹
- 内存缓存、GeoIP、转发器耦合仍然偏高
