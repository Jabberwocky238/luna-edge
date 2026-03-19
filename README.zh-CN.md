# Luna Edge

[English](./README.md)

Luna Edge 是一个面向边缘基础设施的控制面。

它把边缘流量治理抽象为“状态传播”问题：入口行为、DNS 记录、后端绑定、证书状态先在 `master` 统一物化，再以增量变化的方式复制到 `slave`，并保留显式恢复路径，而不是在每个边缘节点反复做全量重建。

这个项目的关注点更接近 platform engineering / DevOps / infra：Kubernetes 输入、控制面物化、副作用编排、复制追平、边缘节点本地执行，被放在一套统一的运行模型里。

## 平台模型

- `master` 是唯一写入控制面
- `slave` 基于本地状态和本地证书文件执行流量服务
- Kubernetes 资源输入与直接 manage API 写入最终收敛到同一套 repository 模型
- 复制链路优先服务于变更流式传播，其次才是 snapshot 恢复
- 边缘节点在控制面远端的情况下仍然以本地状态独立提供服务

## 当前可治理的对象

- 权威 DNS 状态
- HTTP 路由与 TLS 入口行为
- L4 TLS passthrough / TLS termination 拓扑
- 证书申请触发与证书 bundle 分发
- 来自 Kubernetes 资源或直接 plan 的边缘投影状态

## 当前能力

- `master` 使用 SQLite 或 Postgres 保存期望状态
- `master` 暴露 manage API，可供自动化系统直接操作
- `master` 通过 Kubernetes bridge 接收 DNS CRD、`Ingress`、Gateway API 资源
- `master` 在控制面内执行证书调和等副作用
- 复制链路支持 `Subscribe` 实时传播
- 复制链路支持 `GetSnapshot` 恢复追平
- 复制链路支持 `FetchCertificateBundle` 下发证书资产
- `slave` 持有本地 SQLite 缓存和磁盘证书文件
- `slave` 的 DNS 运行时基于复制得到的 `DNSRecord`
- `slave` 的 ingress 运行时基于复制得到的 `DomainEntryProjection`
- `lnctl` 同时提供 Go 库和 CLI 两种控制方式

## 运行流

1. 期望状态通过 Kubernetes bridge 或 manage API 进入 `master`。
2. `master` 将期望状态物化为 repository 模型。
3. 控制面副作用基于已物化状态执行。
4. 增量变更被发布到 `slave`。
5. `slave` 仅依赖本地状态提供服务，必要时通过 snapshot 重新追平。

## 架构图

```text
                    +-----------------------------------+
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

## 核心组件

- `cmd/master`：master 启动入口
- `cmd/slave`：slave 启动入口
- `cmd/lnctl`：面向运维的 manage API CLI
- `lnctl`：用于控制 master 的 Go 库和 plan builder
- `engine/master`：控制面运行时、物化路径、副作用编排与发布路径
- `engine/slave`：本地状态应用与边缘持久化
- `dns`：边缘 DNS 运行时
- `ingress`：边缘 HTTP/TLS 运行时
- `replication`：流式同步、恢复与证书获取 RPC
- `repository`：元数据模型与持久化抽象
- `deploy`：部署清单与环境辅助脚本

## 开发

```bash
go test ./...
```

如果要起本地环境：

```bash
bash <(curl -fsSL https://raw.githubusercontent.com/jabberwocky238/luna-edge/main/deploy/prepare.sh)

bash run.sh up master
bash run.sh up slave

# 可选测试环境
bash run.sh up ngg
bash run.sh up ngi
```

## License

GPL-3.0。见 [LICENSE](./LICENSE)。
