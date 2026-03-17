# repository/connection

## 职责

数据库连接创建与驱动配置封装。

## 架构

- `sqlite.go`: SQLite 连接
- `postgres.go`: Postgres 连接
- `factory.go`: 按配置选择驱动
- `types.go`: 连接配置类型

## 存在的问题

- 目前只负责连接，不负责更高层事务策略
- 配置项仍可能继续增长
