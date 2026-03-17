# engine/master/manage

## 职责
manage API 与控制面写库包装层，负责 REST 管理接口和写库副作用编排。

## 架构
- `api.go`、`router.go` 暴露 HTTP API。
- `wrapper.go` 基于底层 repository 做薄装饰。
- `batch.go` 提供批量写入时的副作用合并。
- `broadcast.go` 定义广播行为。
- `descriptors_registry.go`、`resources.go` 管理资源路由表。

## 存在的问题
- 现在已有批量 effect 合并，但事务边界还未完全纳入。
- 某些资源的副作用仍然较隐式，后续可继续显式化。
