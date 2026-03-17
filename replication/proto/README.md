# replication/proto

## 职责
保存 replication 协议源文件。

## 架构
- `replication.proto` 定义 snapshot stream、change notification stream 和证书拉取接口。

## 存在的问题
- 协议字段近期调整较频繁。
- 需要持续约束不要把主库存储模型直接泄露到复制协议里。
