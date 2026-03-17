# repository/metadata

## 职责
定义主库元数据模型和只读投影结构。

## 架构
- `domain_endpoint.go`、`http_route.go`、`service_binding.go`、`dns_record.go` 定义核心资源。
- `certificate_revision.go`、`snapshot_record.go` 定义证书和复制记录。
- `domain_entry_projection.go` 定义查询投影。
- `models.go` 放共享字段。

## 存在的问题
- 数据模型正处于架构迁移期，字段和语义需要继续收紧。
- 持久化模型与查询投影的边界需要保持明确，避免重新物化膨胀。
