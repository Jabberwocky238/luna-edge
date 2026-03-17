# repository/connection

## 职责
数据库连接创建与配置封装。

## 架构
- `sqlite.go`、`postgres.go` 提供具体连接实现。
- `factory.go` 负责按配置选择驱动。
- `types.go` 定义连接配置类型。

## 存在的问题
- 事务能力还没有对上层显式暴露成统一抽象。
- 连接配置项未来可能继续增长，需要注意复杂度。
