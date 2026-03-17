# repository

## 职责

仓储层总入口，负责数据库连接、元数据模型、Gorm repository 和少量投影查询。

## 架构

- `factory.go`: 创建 repository factory
- `connection/`: 数据库连接
- `functions/`: Gorm repository 实现
- `metadata/`: 持久化模型和投影结构
- `utiltypes/`: 小型辅助类型

## 当前边界

- 主库真实写入模型在这里
- 控制面副作用不在这里做，而是在 `engine/master/manage`
- slave 运行时拿到的是投影结果，不是 repository 直接暴露

## 存在的问题

- 仓储接口既承担通用 CRUD，又承担少量特化查询
- 后续如果引入统一事务，接口还要继续收敛
