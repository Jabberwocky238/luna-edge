# cmd/slave

## 职责
`slave` 进程启动入口，负责组装从节点运行时配置并启动 slave engine。

## 架构
- 解析运行参数。
- 构造 `engine/slave.Engine`。
- 启动 replication 订阅、本地缓存和 ingress/dns 数据面。

## 存在的问题
- slave 启动配置与 master 风格尚未完全统一。
- 本地缓存、证书文件和数据面启动顺序仍可进一步明确。
