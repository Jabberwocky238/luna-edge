# replication

## 职责
复制协议定义与生成脚本目录。

## 架构
- `proto/replication.proto` 定义快照、订阅和证书获取协议。
- `replpb/` 存 protoc 生成代码。
- `generate.sh` 负责代码生成。

## 存在的问题
- 生成产物和 proto 变更需要保持严格同步。
- 协议目前以 `DNSRecord` 和 `DomainEntryProjection` 为主，后续扩展要控制复杂度。
