# replication/replpb

## 职责

保存 replication proto 生成后的 Go 代码。

## 文件

- `replication.pb.go`: 消息定义
- `replication_grpc.pb.go`: gRPC stub

## 约束

- 这里原则上不应手改
- 修改 `proto` 后应尽快重新生成

## 当前风险

- 如果 proto 已更新但生成文件未同步，会直接造成复制链路语义漂移
