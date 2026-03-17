# engine/slave

## 职责

slave 运行时核心，负责订阅 master 复制流、维护本地缓存、同步证书文件，并刷新 DNS / ingress 数据面。

## 架构

- `engine.go`: slave 生命周期、Subscribe、catch-up、notice 应用
- `store.go`: 本地存储聚合入口
- `store_metadata.go`: 本地 SQLite 元数据缓存
- `store_certfiles.go`: 本地证书文件同步

## 当前复制模型

- 启动时先读取本地 `snapshot_record_id`
- `Subscribe` 建立实时 notice 流
- 初始追平或发现 gap 时才调用 `GetSnapshot`
- notice 正常情况下直接转成单条本地 snapshot 应用

## 当前缓存语义

- `DNSRecord` 支持 `deleted`
- `DomainEntryProjection` 支持 `deleted`
- slave 收到 delete changelog 后直接删本地缓存行
- 证书文件根据活跃 hostname 集合同步和清理

## 存在的问题

- 运行时刷新链路已经清晰，但仍然有不少日志和诊断代码
- 本地存储可以继续压缩重复字段和重复序列化路径
