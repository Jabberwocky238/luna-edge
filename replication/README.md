# replication

## 职责

复制协议定义、生成脚本和生成代码目录。

## 当前协议边界

- `Subscribe`: 实时 changelog 流
- `GetSnapshot`: 恢复流
- `FetchCertificateBundle`: 证书文件拉取

消息核心只保留：

- `DNSRecord`
- `DomainEntryProjection`

并且两者都支持 `deleted` 语义。

## 目录

- `proto/`: proto 源文件
- `replpb/`: 生成产物
- `generate.sh`: 代码生成脚本

## 存在的问题

- proto 和生成产物必须严格同步
- 当前 `replpb` 有手动补丁时，必须尽快重新生成归一
