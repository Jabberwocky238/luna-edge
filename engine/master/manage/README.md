# engine/master/manage

## 职责

manage API 与主库写包装层，负责把“写数据库”扩展成“写数据库 + 副作用 + 复制广播”。

## 架构

- `api.go`、`router.go`: HTTP API
- `wrapper.go`: repository 包装
- `batch.go`: 批处理入口，合并副作用
- `broadcast.go`: changelog 发布逻辑
- `descriptors_registry.go`: 资源路由表

## 当前设计

- `Wrapper` 尽量复用 `repository/functions/gorm_generic_repository.go`
- 正常写路径通过 `Wrapper` 触发副作用
- delete 语义通过显式 `deleted` changelog 下发给 slave
- `Batch(ctx, fn)` 用于把一组物化更新合成一次副作用收口

## 存在的问题

- 批处理已经解决副作用合并，但底层事务仍未完全统一
- 某些资源的隐式联动仍然偏多
