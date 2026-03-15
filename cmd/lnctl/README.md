# Debug Master With `lnctl`

`lnctl` 是一个直接打 `master` manage API 的调试工具。它适合做两类事情：

- 看 `master` 当前数据库里到底有什么
- 手工核对某个域名为什么没有生成正确的路由、证书或节点挂载

这个文档默认你使用的是 Dockerfile 构建出来的二进制，也就是镜像里的：

```bash
/usr/local/bin/lnctl
```

下面示例直接写成 `lnctl`。

默认管理地址是：

```bash
http://127.0.0.1:8080
```

也可以显式指定：

```bash
lnctl --master http://<master-host>:8080 ...
```

或者：

```bash
export LUNA_MASTER_MANAGE_URL=http://<master-host>:8080
```

## 如何进入 master Pod

如果你是在 Kubernetes 里调试，先找到 `master` Pod：

```bash
kubectl -n luna-edge get pods -l app=luna-master -o wide
```

进入 Pod：

```bash
kubectl -n luna-edge exec -it deploy/luna-master -- sh
```

如果你更想精确进某个 Pod，也可以先取名字：

```bash
kubectl -n luna-edge get pods -l app=luna-master
kubectl -n luna-edge exec -it <master-pod-name> -- sh
```

进入以后最先看这几个东西：

```bash
env | sort
ps aux
```

重点确认：

- `LUNA_POSTGRES_DSN`
- `LUNA_S3_ENDPOINT`
- `LUNA_S3_REGION`
- `LUNA_S3_ACCESS_KEY_ID`
- `LUNA_MANAGE_LISTEN`
- `LUNA_REPLICATION_LISTEN`

如果你只是想看日志，通常不需要进 Pod，直接：

```bash
kubectl -n luna-edge logs deploy/luna-master
```

持续跟日志：

```bash
kubectl -n luna-edge logs -f deploy/luna-master
```

## 先确认 master 活着

如果你在本地调试：

```bash
curl http://127.0.0.1:8080/healthz
```

如果你在集群里调试：

```bash
kubectl -n luna-edge port-forward svc/luna-master 8080:8080
curl http://127.0.0.1:8080/healthz
```

返回 `ok` 再继续。

## 常用资源

先看工具支持哪些资源：

```bash
lnctl --help
```

最常用的是：

- `nodes`
- `attachments`
- `domain_endpoints`
- `domain_endpoint_status`
- `route_projections`
- `service_bindings`
- `certificate_revisions`
- `dns_records`

## 最常见的排查顺序

### 1. 先看节点有没有注册

```bash
lnctl nodes ls
```

如果这里没有目标节点，后面的挂载和复制都不用看了。

### 2. 看节点有没有拿到 attachment

```bash
lnctl attachments ls
```

重点看：

- `node_id`
- `domain_id`
- `listener`
- `desired_route_version`
- `desired_certificate_revision`
- `desired_dns_version`
- `state`
- `last_error`

如果 attachment 都没有，说明 `master` 侧还没把域名挂到这个节点。

### 3. 看域名主对象

```bash
lnctl domain_endpoints ls
lnctl domain_endpoints get <domain-id>
```

这里确认：

- 域名对象是否存在
- `hostname` 是否正确
- `state_version` 是否在推进

### 4. 看路由投影和服务绑定

```bash
lnctl route_projections ls
lnctl service_bindings ls
```

这两张表一起看：

- `route_projections` 决定 hostname 到 binding 的映射
- `service_bindings` 决定最终回源地址、端口、协议

如果域名存在，但这里没有对应 projection 或 binding，`slave` 就算连上 `master` 也不会有正确转发。

### 5. 看证书版本

```bash
lnctl certificate_revisions ls
```

重点看：

- `hostname`
- `revision`
- `status`
- `artifact_bucket`
- `artifact_prefix`

如果 `artifact_bucket` / `artifact_prefix` 不对，`master` 返回证书文件时就会失败。

### 6. 看域名状态

```bash
lnctl domain_endpoint_status ls
```

重点看：

- `certificate_revision`
- `certificate_ready`
- `ready`
- `phase`

这能帮助判断问题是在声明态、投影态，还是证书态。

### 7. 看 DNS 记录

```bash
lnctl dns_records ls
```

如果你怀疑 DNS 没生成，就看这里，而不是先去怀疑 `slave`。

## 定位某个 hostname 的最短路径

假设你在查 `app.example.com`：

1. `domain_endpoints ls` 里先确认有没有这个 hostname
2. 找到对应 `domain_id`
3. 用这个 `domain_id` 去看：
   - `route_projections`
   - `service_bindings`
   - `certificate_revisions`
   - `attachments`
4. 最后再看 `domain_endpoint_status`

如果这几张表都对，问题才更可能在复制或 `slave` 运行时。

## 修改资源做临时验证

`lnctl` 也支持直接 `put`：

```bash
lnctl service_bindings put -d '{"id":"binding-1","domain_id":"domain-1","hostname":"app.example.com","service_id":"svc-1","namespace":"default","name":"svc-app","address":"10.0.0.9","port":8080,"protocol":"http","route_version":9,"backend_json":"{}"}'
```

或者从文件读：

```bash
lnctl service_bindings put -f binding.json
```

删除资源：

```bash
lnctl attachments rm <attachment-id>
```

调试时这样做很方便，但要清楚它是直接改 `master` 数据，不是只读命令。

## 集群里最实用的调试方式

```bash
kubectl -n luna-edge port-forward svc/luna-master 8080:8080
lnctl attachments ls
lnctl route_projections ls
lnctl service_bindings ls
lnctl certificate_revisions ls
```

这套最适合先判断问题是在 `master` 数据层，还是在 `slave` 收敛层。
