# deploy

这个目录保存 Kubernetes 部署文件和辅助脚本。

## 当前思路

- `master` 部署在控制面集群
- `slave` 作为数据面节点常驻
- `master` 自己监听 Kubernetes 资源并写主库
- `slave` 不再承担控制面物化职责

## 主要文件

- [ns.yaml](/mnt/d/1-code/__trash__/luna-edge/deploy/ns.yaml): namespace 与基础对象
- [luna-edge-master.yaml](/mnt/d/1-code/__trash__/luna-edge/deploy/luna-edge-master.yaml): master Deployment / Service / RBAC
- [luna-edge-slave.yaml](/mnt/d/1-code/__trash__/luna-edge/deploy/luna-edge-slave.yaml): slave DaemonSet
- [luna-edge-master-cilium-clustermesh.yaml](/mnt/d/1-code/__trash__/luna-edge/deploy/luna-edge-master-cilium-clustermesh.yaml): ClusterMesh 下的 master
- [luna-edge-slave-cilium-clustermesh.yaml](/mnt/d/1-code/__trash__/luna-edge/deploy/luna-edge-slave-cilium-clustermesh.yaml): ClusterMesh 下的 slave
- [luna-edge-nginx-ingress.yaml](/mnt/d/1-code/__trash__/luna-edge/deploy/luna-edge-nginx-ingress.yaml): Ingress 兼容性验证
- [luna-edge-nginx-gateway.yaml](/mnt/d/1-code/__trash__/luna-edge/deploy/luna-edge-nginx-gateway.yaml): Gateway API 兼容性验证

## 关键约束

- master 需要有权限 watch `Ingress`、Gateway API、DNS CRD
- slave 使用本地缓存和证书目录运行
- slave 通常使用 `hostNetwork: true` 直接监听 `53/80/443`
- master 和 slave 都需要注入：
  - `POD_IP`
  - `POD_NAMESPACE`
  - `POD_NAME`

## 证书相关

- ACME http-01 challenge 由 master HTTP 服务直接响应
- slave 不负责 challenge 物化
- 证书 bundle 通过 replication RPC 从 master 拉取

## 使用前检查

1. 校验 master 的 RBAC 是否包含 DNS CRD、Ingress、Gateway API
2. 校验集群是否安装了需要的 Gateway API CRD 版本
3. 校验 slave 所在节点没有别的进程占用 `53/80/443`
4. 校验证书存储和数据库配置已经替换成真实值

## 存在的问题

- 部署文件仍然偏手工，环境变量较多
- 不同集群模式的模板还可以继续收敛
- 某些 CRD 版本差异仍需人工对齐
