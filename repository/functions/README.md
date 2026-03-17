# repository/functions

## 职责
基于 Gorm 的 repository 实现，包括 generic CRUD 和少量特化查询。

## 架构
- `gorm_generic_repository.go` 提供通用 CRUD 基座。
- 其余 `gorm_*_repository.go` 提供资源特化查询。
- `gorm_repository.go` 负责聚合总仓储。
- `interfaces.go` 定义仓储接口。

## 存在的问题
- 特化查询和 generic CRUD 的边界需要持续控制。
- 逻辑删除已经引入，后续所有查询都必须持续遵循同一约束。
