# engine/master/acme

## 职责

ACME 证书申请子系统，负责下单、challenge 生命周期、证书持久化、bundle 输出和复制后续动作。

## 架构

- `service.go`: 主入口
- `http01.go`: http-01 challenge 注册与清理
- `dns01.go`: dns-01 challenge 物化
- `issuer_lego.go`: 基于 lego 的 issuer
- `bundle.go`: 证书 bundle 组装
- `types.go`: 接口定义

## 当前设计

- http-01 challenge 直接由 master HTTP 服务响应
- 证书签发成功后先写主库，再写 bundle，再更新域名绑定，再发布复制变更
- slave 不参与 challenge 物化

## 存在的问题

- ACME 成功后的多个副作用步骤仍然偏串行、偏重
- 如果 bundle 存储策略继续调整，这里还需要继续收敛接口
