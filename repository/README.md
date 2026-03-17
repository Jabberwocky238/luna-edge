# repository

## 职责
仓储层总入口，负责连接、通用 repository 接口、元数据模型和 gorm 实现。

## 架构
- `factory.go` 负责创建统一 repository factory。
- `connection/` 管理数据库连接。
- `functions/` 提供 gorm repository 实现。
- `metadata/` 定义持久化模型和投影结构。
- `utiltypes/` 提供少量辅助类型。

## 存在的问题
- 当前 repository 同时承担原始持久化和少量特化查询，边界仍在调整。
- 与 manage wrapper 的职责分层需要继续保持清晰。
