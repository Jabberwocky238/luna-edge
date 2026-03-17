# cmd/slave

## 职责

`slave` 进程启动入口，负责读取配置、构造 slave engine，并启动 DNS / ingress / replication。

## 当前启动内容

- 本地缓存根目录
- replication 订阅客户端
- 本地 SQLite store
- 本地证书目录
- DNS / ingress 数据面

## 存在的问题

- 启动参数仍有进一步收敛空间
- 运行时依赖关系主要靠 engine 层维持
