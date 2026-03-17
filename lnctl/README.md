# lnctl

## 职责

控制面客户端库，封装对 master manage API 的资源操作。

## 架构

- `client.go`: 资源客户端
- `resources.go`: 当前支持的资源集合
- 被 `cmd/lnctl` 直接复用

## 当前资源模型

- `certificate_revisions`
- `dns_records`
- `domain_endpoints`
- `http_routes`
- `service_backend_refs`
- `snapshot_records`

## 存在的问题

- 资源集合仍然需要人工同步
- 目前偏底层，缺少组合型运维操作
