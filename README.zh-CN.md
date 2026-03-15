# Luna Edge

[English](./README.md)

Luna Edge 是一个面向域名入口流量的统一边缘系统。它把 DNS、Ingress、TLS、证书分发和节点激活放进同一个控制模型里，而不是拆成互不相关的几套系统。

## 它解决什么问题

针对单个 hostname，Luna Edge 可以统一管理：

- DNS 发布
- 流量路由
- 上游服务绑定
- TLS 终止
- 证书签发、续期与分发
- 节点级激活

## 核心思路

Luna Edge 把一个域名入口视为一个完整对象。

`master` 持有期望状态，并计算每个 `slave` 应该运行的最终物化状态。`slave` 不关心中间变化，只关心尽快收敛到最新最终状态。

因此复制模型是基于版本的，而不是基于事件回放的：

- 每个节点维护一个 `VersionVector`
- 保留订阅机制用于实时推送
- 真正的恢复和对账通过 `GetSnapshot`
- 当前主设计里没有 event log 和 cursor replay

## 架构

### 控制面

- `master` 接收写入
- `master` 使用 SQLite 或 Postgres 保存元数据
- `master` 基于 repository projection 构建每个节点的快照
- `master` 向 slave 推送轻量级变更通知
- `master` 通过 `FetchCertificateBundle` 提供证书文件下载

### 数据面

- `slave` 持有本地物化存储
- `slave` 带着自己的 `VersionVector` 订阅 master
- 当 master 发现版本更新时，`slave` 拉取最新快照并整体替换本地状态
- `slave` 基于本地状态运行 DNS 和 ingress
- `slave` 从 `master` 拉取证书文件，写入本地 cert root，再由本地 ingress 使用

### 复制模型

复制围绕“最终态收敛”设计：

1. `slave` 读取本地版本。
2. `slave` 建立 `Subscribe(node_id, known_versions)`。
3. `master` 比较版本，发现节点需要刷新时发送 `ChangeNotification`。
4. `slave` 调用 `GetSnapshot(node_id)`。
5. `slave` 在一个事务里替换本地快照状态。
6. DNS、Ingress、证书运行时从本地状态刷新。

这样保留了推送带来的低延迟，同时避免让 slave 回放所有中间事件。

### 证书链路

- `master` 在 repository 中保存证书元数据
- 实际的 `tls.crt`、`tls.key`、`metadata.json` 通过复制 RPC 获取
- `slave` 侧证书同步由 `engine/slave/CertManager` 负责
- ingress 只从本地磁盘加载证书

### TLS Resolver

Ingress 的 TLS resolver 有明确约束：

- hostname 在映射到文件路径前会先做 sanitizer
- cert root 必须是合法且非空的目录
- 文件系统 watcher 的唯一职责是让受影响缓存项失效
- watcher 不会预热缓存，也不会整表清空缓存
- watcher 分为 Windows 和非 Windows 两套实现

## 主要组件

- `cmd/master`：master 启动入口
- `cmd/slave`：slave 启动入口
- `engine/master`：控制面引擎与复制服务
- `engine/slave`：副本引擎、本地存储、证书管理
- `dns/`：权威 DNS 运行时
- `ingress/`：HTTP/TLS Ingress 运行时
- `replication/`：protobuf 定义与 gRPC 生成代码
- `repository/`：元数据仓储与存储抽象
- `lnctl/`：控制与客户端辅助工具

## 存储设计

- `master`：SQLite 适合本地/单机场景，Postgres 适合集中控制面
- `slave`：SQLite 保存本地物化元数据
- 证书文件不放在元数据库里，而是作为文件同步到本地

## 当前运行假设

- `master` 是唯一写入者
- `slave` 对期望状态只读
- 订阅负责实时性，快照负责真相
- 即使与 master 短暂断连，slave 仍应基于本地状态继续提供运行时能力

## 开发

运行全量测试：

```bash
go test ./...
```

## License

MIT。见 [LICENSE](./LICENSE)。
