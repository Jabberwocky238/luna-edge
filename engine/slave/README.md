# engine/slave

## 职责
slave 运行时核心，负责从 master 同步快照和变更，并维护本地 sqlite 缓存与证书文件。

## 架构
- `engine.go` 负责 slave 生命周期和 replication 订阅。
- `store.go` 聚合本地存储。
- `store_metadata.go` 存元数据详细 JSON。
- `store_certfiles.go` 存证书文件。

## 存在的问题
- slave 本地存储方案刚完成拆分，仍需继续压缩冗余。
- replication 应用路径和缓存对外查询接口还可以更清晰。
