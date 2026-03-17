# engine/master/acme

## 职责
ACME 证书申请子系统，负责下单、挑战、证书落库和证书包生成。

## 架构
- `service.go` 是主服务入口。
- `http01.go`、`dns01.go` 分别实现两类挑战物化。
- `issuer_lego.go` 对接 lego 颁发器。
- `bundle.go` 负责证书 bundle 生成。
- `types.go`、`challenge_provider.go` 定义接口和挑战提供器。

## 存在的问题
- 证书申请、主库存储和副作用广播之间仍有耦合。
- 测试覆盖在真实外部行为和内部状态之间还不够平衡。
