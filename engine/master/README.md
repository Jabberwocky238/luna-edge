# engine/master

## 职责

master 控制面核心，负责主库存储、Kubernetes 监听、manage API、副作用编排、复制发布和证书调和。

## 架构

- `engine.go`: 聚合 master 生命周期
- `hub.go`: 维护订阅连接并广播 changelog
- `cert_reconciler.go`: 周期性扫描 `NeedCert` 与证书有效期，并支持 `NotifyCertificateDesired`
- `manage/`: 对主库写操作做副作用包装
- `k8s_bridge/`: 监听 Ingress / Gateway / DNS CRD 并写主库
- `acme/`: 证书申请与 challenge 处理

## 当前复制模型

- `PublishChangeLog` 是正常实时路径
- `PublishSnapshot` 只保留给恢复语义
- `GetSnapshot` 负责按 `snapshot_record_id` 恢复
- `Subscribe` 负责实时推送单条变更

## 存在的问题

- 控制面职责很多，边界虽然已经比之前清晰，但仍然偏重
- `PublishSnapshot` 和恢复逻辑后续还可以继续收窄
