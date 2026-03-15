# Luna Edge 部署说明

部署文件已经拆成四份，按场景分别维护：

- [ns.yaml](/mnt/d/1-code/__trash__/luna-edge/deploy/ns.yaml)
- [luna-edge-master.yaml](/mnt/d/1-code/__trash__/luna-edge/deploy/luna-edge-master.yaml)
- [luna-edge-slave.yaml](/mnt/d/1-code/__trash__/luna-edge/deploy/luna-edge-slave.yaml)
- [luna-edge-master-cilium-clustermesh.yaml](/mnt/d/1-code/__trash__/luna-edge/deploy/luna-edge-master-cilium-clustermesh.yaml)
- [luna-edge-slave-cilium-clustermesh.yaml](/mnt/d/1-code/__trash__/luna-edge/deploy/luna-edge-slave-cilium-clustermesh.yaml)
- [nginx-ingress.yaml](/mnt/d/1-code/__trash__/luna-edge/deploy/nginx-ingress.yaml)
- [nginx-gateway.yaml](/mnt/d/1-code/__trash__/luna-edge/deploy/nginx-gateway.yaml)

## 文件用途

普通部署：

- `ns.yaml`：创建 `luna-edge` namespace，并初始化 `GatewayClass` / `Gateway`
- `luna-edge-master.yaml`：部署单集群 `master`
- `luna-edge-slave.yaml`：部署单集群 `slave` DaemonSet 和本地入口资源
- `nginx-ingress.yaml`：使用标准 `Ingress` 验证兼容性
- `nginx-gateway.yaml`：使用 `HTTPRoute` 验证 Gateway API 适配性

Cilium ClusterMesh：

- `luna-edge-master-cilium-clustermesh.yaml`：部署核心集群 `master`
- `luna-edge-slave-cilium-clustermesh.yaml`：部署边缘集群 `slave` DaemonSet

当前推荐拓扑是：

- `master` 只在核心集群部署
- 每个边缘集群至少有一个 `slave` Pod 存活
- 如果希望入口铺满本集群节点，就保留 `DaemonSet`
- 如果只想跑在一部分入口节点，就给 `DaemonSet` 加 `nodeSelector` / `tolerations`

## 普通部署

先准备部署文件：

```bash
bash <(curl -fsSL https://raw.githubusercontent.com/jabberwocky238/luna-edge/main/deploy/prepare.sh)
```

准备完成后，在 `deploy/` 目录执行：

```bash
./run.sh up ns
./run.sh up master
./run.sh up slave
./run.sh up ngi
./run.sh up ngg
```

需要替换的值：

- 镜像地址
- Postgres DSN
- S3 endpoint / region / access key / secret key
- `hostPath` 挂载路径

## Cilium ClusterMesh 部署

同样先准备部署文件：

```bash
bash <(curl -fsSL https://raw.githubusercontent.com/jabberwocky238/luna-edge/main/deploy/prepare.sh)
```

核心集群：

```bash
./run.sh mode cilium
./run.sh up ns
./run.sh up master
./run.sh up ngi
./run.sh up ngg
```

每个边缘集群：

```bash
./run.sh mode cilium
./run.sh up ns
./run.sh up slave
./run.sh up ngi
./run.sh up ngg
```

边缘集群里必须改的值：

- `LUNA_CLUSTER_NAME`
- `LUNA_MASTER_ADDRESS`
- `hostPath` / 调度约束

前提假设：

- 集群之间网络已经通过 Cilium ClusterMesh 打通
- 边缘集群可以直接访问核心集群中的 `master`
- 证书仍然由核心集群 `master` 通过复制 RPC 提供

如果你要切回默认单集群模式：

```bash
./run.sh mode default
```

## 手动清除 Namespace

正常清理顺序：

```bash
./run.sh down ngg
./run.sh down ngi
./run.sh down slave
./run.sh down master
./run.sh down ns
```

如果 `luna-edge` namespace 卡在 `Terminating`，先看里面还有没有残留对象：

```bash
kubectl get all -n luna-edge
kubectl get ingress,configmap,secret,serviceaccount,role,rolebinding -n luna-edge
```

如果还有残留，先删干净：

```bash
kubectl delete ingress,configmap,secret,service,serviceaccount,role,rolebinding,deploy,ds -n luna-edge --all
```

如果对象已经没了，但 namespace 还是不退出，就把 finalizer 清掉：

```bash
kubectl get ns luna-edge -o json > luna-edge-ns.json
```

编辑 `luna-edge-ns.json`，把下面这段改成空数组：

```json
"spec": {
  "finalizers": []
}
```

然后执行：

```bash
kubectl replace --raw "/api/v1/namespaces/luna-edge/finalize" -f ./luna-edge-ns.json
```

最后确认：

```bash
kubectl get ns luna-edge
```

## 如何替换现有 Ingress

这里默认说的是 `k3s` 自带 `Traefik` 的情况，而且默认你的集群刚启动、还没进入生产。

这种情况下，不需要把迁移动作设计得太复杂，做法就是把准备交给 Luna Edge 的 Ingress 从 `Traefik` 切到 `luna-edge`。

推荐顺序：

1. 先部署对应的 `master` / `slave` 文件。
2. 确认 `master` Ready。
3. 确认目标集群里至少一个 `slave` Pod Ready。
4. 确认 `Service/luna-edge` 已拿到外部地址，或者你的上游 LB 已经能把流量送进 `luna-edge`。
5. 确认 `LUNA_INGRESS_K8S_NAMESPACE` 指向你准备接管的 namespace。
6. 找到原本由 k3s `Traefik` 接管的旧 Ingress，让它不再对流量产生影响。
7. 部署新的、由 `luna-edge` 接管的 Ingress。
8. 验证域名访问、证书加载、后端转发都正常。

因为还没进生产，通常不需要保留双栈切流，也不需要做灰度。重点只有两件事：

1. 去掉老 Ingress 的影响。
2. 让新的 `luna-edge` Ingress 立即接管。

示例：

```yaml
apiVersion: networking.k8s.io/v1
kind: Ingress
metadata:
  name: app
  namespace: luna-edge
spec:
  ingressClassName: luna-edge
  rules:
    - host: app.example.com
      http:
        paths:
          - path: /
            pathType: Prefix
            backend:
              service:
                name: app
                port:
                  number: 8080
```

## 说明

- 当前 `slave` 仍然只 watch 一个 namespace。
- 默认部署会同时开启 DNS、HTTP 和 HTTPS 入口。
- `slave` DaemonSet 现在使用 `hostNetwork: true`，会直接占用宿主机 `:53` / `:80` / `:443`。
- 这意味着不再依赖 k3s 默认 `servicelb`、`traefik` 或其他 `LoadBalancer` 实现，宿主机可以直接访问这些端口。
- 如果节点上已经有别的进程占用了 `53/80/443`，`slave` Pod 会因为端口冲突而无法启动。
- `slave` 的 ingress 监听固定就是 `:80` / `:443`，并且固定开启 k8s ingress bridge；部署层不再暴露这些开关。
- `slave` 默认把整个 `/var/lib/luna` 挂到宿主机 `/data/luna-edge`。
- 代码里固定使用 `/var/lib/luna/meta.db` 和 `/var/lib/luna/certs`，外部只需要配置 `LUNA_CACHE_ROOT` 这个根目录。
- `master` 读取证书依赖数据库中的 `artifact_bucket` 和 `artifact_prefix`。
- 如果你用的是 k3s，`Traefik` 默认可能已经安装好；Luna Edge 接管时，本质上是改业务 Ingress 的 `ingressClassName`，不是必须先删除 `Traefik` 这个组件。

## DNS 配置

当前部署文件已经把代码里支持的主要 DNS 环境变量都显式暴露在 `slave` 的 `ConfigMap` 里：

- `LUNA_DNS_LISTEN`
- `LUNA_DNS_FORWARD_ENABLED`
- `LUNA_DNS_FORWARD_SERVERS`
- `LUNA_DNS_FORWARD_TIMEOUT`
- `LUNA_DNS_GEOIP_ENABLED`
- `LUNA_DNS_GEOIP_MMDB_PATH`

默认值是：

- DNS 监听 `:53`
- 不开启上游转发
- 默认转发目标 `1.1.1.1:53`
- 默认转发超时 `5s`
- 默认开启 GeoIP 排序
- 默认 mmdb 路径 `/var/lib/luna/geoip/GeoLite2-City.mmdb`

如果你要把本地未命中的请求转发到上游解析器，直接把 `LUNA_DNS_FORWARD_ENABLED` 改成 `"1"` 即可。

另外，部署文件里的 `slave` DaemonSet 默认带一个 `initContainer`：

- 在主容器启动前检查 `/var/lib/luna/geoip/GeoLite2-City.mmdb`
- 如果本地没有，就自动下载
- 文件最终会落到宿主机 `/data/luna-edge/geoip/GeoLite2-City.mmdb`

这里没有用单个 `Job`，因为当前是 `DaemonSet + hostPath`，mmdb 必须在每个运行 `slave` 的节点本地都存在，最稳妥的方式就是让每个 `slave` Pod 自己在启动前准备。
