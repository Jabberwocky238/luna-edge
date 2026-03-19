# lnctl

`lnctl` 是面向 luna-edge master manage API 的 Go 客户端库。

它的职责不是做通用数据库 CRUD，而是提供两类更贴近控制面的能力：

- `Client`: 直接调用 master 的查询和 plan 提交接口
- `Builder`: 根据期望状态构造 `Plan`，用于提交到 master 落库和广播

## 当前能力

- 查询域名投影：`QueryDomainEntryProjection`
- 查询 DNS 记录：`QueryDNSRecords`
- 提交 plan：`ApplyPlan`
- 构造 plan：`NewBuilder(...).Build()`

## 典型使用方式

1. 用 `Client` 从 master 查询当前状态
2. 用 `Builder` 根据“当前状态 + 目标状态”生成 `Plan`
3. 再用 `Client.ApplyPlan(...)` 把 plan 提交给 master

## 文件说明

- `client.go`: master manage API 客户端
- `builder.go`: plan 构造器和 diff 逻辑
- `client_query_test.go`: client 查询和 apply 测试
- `builder_test.go`: builder 构造和 diff 测试
- `usage.md`: Go 代码调用示例

## 相关接口

当前对应的 master 接口包括：

- `GET /manage/query/domain-entry-projection`
- `GET /manage/query/dns-records`
- `POST /manage/plan`

如果你要看完整示例，直接看 [usage.md](/mnt/d/1-code/__trash__/luna-edge/lnctl/usage.md)。
