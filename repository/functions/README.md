# repository/functions

## 职责

基于 Gorm 的 repository 实现。

## 架构

- `gorm_generic_repository.go`: 通用 CRUD 基座
- `gorm_repository.go`: 聚合总 repository
- 其余 `gorm_*_repository.go`: 特化查询
- `interfaces.go`: 仓储接口

## 当前设计

- 新架构尽量复用 generic repository
- manage wrapper 只在外层增加副作用，不重复实现一套 repository
- 逻辑删除由共享查询约束统一处理

## 存在的问题

- 特化查询仍然分散
- 如果引入事务包装，接口层还需要继续整理
