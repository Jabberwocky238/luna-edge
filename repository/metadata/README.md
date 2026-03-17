# repository/metadata

## 职责

定义主库持久化模型、复制记录模型和只读投影结构。

## 核心对象

- `DomainEndpoint`
- `HTTPRoute`
- `ServiceBackendRef`
- `DNSRecord`
- `CertificateRevision`
- `SnapshotRecord`
- `DomainEntryProjection`

## 当前语义

- `Shared` 提供逻辑删除和时间戳
- `DNSRecord` 复制时使用 `Deleted`
- `DomainEntryProjection` 是只读投影，也带 `Deleted`，用于 slave 删除域名缓存

## 存在的问题

- 当前正处于架构迁移期，字段语义还在收紧
- 持久化模型和投影模型必须继续保持边界
