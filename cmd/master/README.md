# cmd/master

## 职责

`master` 进程启动入口，负责读取配置、构造 master engine，并启动控制面服务。

## 当前启动内容

- repository factory
- manage API
- replication gRPC
- cert reconciler
- master k8s bridge
- ACME service

## 存在的问题

- 配置项仍然偏多
- 入口层仍然承担部分装配细节
