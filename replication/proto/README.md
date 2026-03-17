# replication/proto

## 职责

保存 replication 协议源文件。

## 当前模型

- `Snapshot` 用于恢复
- `ChangeNotification` 用于实时同步
- `DNSRecord.deleted` 表示删除 DNS 记录
- `DomainEntryProjection.deleted` 表示删除域名投影

## 设计约束

- 不把整个主库存储模型直接暴露给 slave
- 只传 slave 运行时真正需要的物化结果

## 存在的问题

- 协议仍在迁移期，字段调整较频繁
