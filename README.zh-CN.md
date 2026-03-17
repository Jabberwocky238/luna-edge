# Luna Edge

[English](./README.md)

Luna Edge 是一个云原生、多集群的边缘融合网关。

它把 DNS、HTTP Ingress、TLS 终止、证书申请、证书分发、Kubernetes 物化统一到同一个控制面里。

它支持 Kubernetes `Ingress` 与 2026 年实验性 Gateway API 能力；由于 repository 模型与 Kubernetes 物化深度融合，它可以实现类似 Quicksilver 的效果：控制面状态只写一次、物化一次，再以小粒度 changelog 推送到边缘节点，而不是每次都重建整份全量状态。

`master` 同时支持两种控制输入：

- Kubernetes 资源通过内建 bridge 直接控制 master
- 管理端点通过内置 `lnctl` client 直接控制 master

## 定位

- 云原生边缘融合网关
- 多集群控制与边缘分发
- Kubernetes 原生物化模型
- Kubernetes 可以直接通过 bridge 控制 master
- 支持 2026 年实验性 Gateway API
- 基于 repository + replication 的 Quicksilver 风格增量传播
- 支持通过 `lnctl` 完整控制 manage 端点

## 架构图

```text
     Kubernetes Ingress / Gateway API              lnctl / manage API
                    |                                   |
                    +-----------------+-----------------+
                                      |
                                      v
                    +-----------------------------+
                    |           master            |
                    |  k8s bridge + repository    |
                    |  cert reconcile + ACME      |
                    |  changelog publisher        |
                    +-------------+---------------+
                                  |
                     Subscribe / GetSnapshot / FetchCertificateBundle
                                  |
                +-----------------+-----------------+
                |                                   |
                v                                   v
      +---------------------+             +---------------------+
      |       slave A       |             |       slave B       |
      | local sqlite cache  |             | local sqlite cache  |
      | cert files on disk  |             | cert files on disk  |
      | DNS + ingress serve |             | DNS + ingress serve |
      +---------------------+             +---------------------+
```

## 当前架构

- `master` 是唯一写入者。
- `master` 使用 SQLite 或 Postgres 保存期望状态。
- `master` 通过 `engine/master/k8s_bridge` 监听 Kubernetes 资源。
- `master` 先把声明物化到 repository，再触发证书等副作用，最后发布复制变更。
- `slave` 持有本地 SQLite 缓存和磁盘证书文件。
- `slave` 的 DNS 和 ingress 只读取本地状态运行。

## 复制模型

复制已经明确拆成两条路径：

- `Subscribe` 是实时同步主路径。
  每次只发送一条 `ChangeNotification`。
- `GetSnapshot` 是异常恢复路径。
  只在首次追平或发现 `snapshot_record_id` 缺口时使用。

正常稳态更新不应该滥用全量快照。

当前复制载荷约束：

- `DNSRecord` 带 `deleted`
- `DomainEntryProjection` 带 `deleted`
- slave 按增量直接删除或覆盖本地缓存行

## 证书链路

- `master` 通过 cert reconciler 判断某个 hostname 是否需要申请或续期。
- ACME 的 http-01 challenge 直接由 master 的 HTTP 服务提供。
- 证书元数据写入主库。
- 证书 bundle 字节通过复制 RPC `FetchCertificateBundle` 提供给 slave。
- slave 把 `tls.crt`、`tls.key`、`metadata.json` 写到本地证书目录。

## Kubernetes 链路

Kubernetes 监听现在由 `master` 独占：

- `engine/master/k8s_bridge/dns.go` 监听 DNS CRD 并写 `DNSRecord`
- `engine/master/k8s_bridge/ingress.go` 监听 `Ingress`
- `engine/master/k8s_bridge/gateway*.go` 监听 Gateway API

统一顺序是：

1. 写 master 主库
2. 处理副作用，例如证书调和
3. 发布复制 changelog

## 主要目录

- `cmd/master`：master 启动入口
- `cmd/slave`：slave 启动入口
- `cmd/lnctl`：manage API 命令行工具
- `engine/master`：控制面运行时
- `engine/slave`：slave 运行时与本地存储
- `dns`：权威 DNS 运行时
- `ingress`：HTTP/TLS 运行时
- `replication`：protobuf 与 RPC 生成代码
- `repository`：存储接口、元数据模型和 Gorm 实现
- `deploy`：Kubernetes 部署文件

## 状态说明

- 当前仍处于架构迁移期
- 兼容旧模型不是首要目标
- 具体模块职责和问题请看各子目录自己的 `README.md`

## 开发

```bash
go test ./...
```

## License

GPL-3.0。见 [LICENSE](./LICENSE)。
