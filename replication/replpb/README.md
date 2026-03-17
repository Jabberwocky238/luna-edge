# replication/replpb

## 职责
保存 replication proto 生成后的 Go 代码。

## 架构
- `replication.pb.go` 是消息定义。
- `replication_grpc.pb.go` 是 gRPC stub。

## 存在的问题
- 该目录是生成产物，不应手改。
- 生成脚本、`go_package` 和输出目录稍有不一致就容易污染路径结构。
