# lnctl

## 职责
控制面客户端库，封装对 master manage API 的资源管理调用。

## 架构
- `client.go` 提供 typed resource client。
- `resources.go` 维护当前支持的资源集合。
- 由 `cmd/lnctl` 复用。

## 存在的问题
- 与最新数据模型的同步依赖人工维护。
- 当前只覆盖 CRUD，尚未抽象更高层次的控制面操作。
