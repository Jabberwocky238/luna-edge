# cmd/lnctl

`lnctl` 是直接访问 master manage API 的命令行工具，主要用于调试主库里的物化结果。

## 当前支持的资源

- `certificate_revisions`
- `dns_records`
- `domain_endpoints`
- `http_routes`
- `service_backend_refs`
- `snapshot_records`

这份列表和 [`lnctl/resources.go`](/mnt/d/1-code/__trash__/luna-edge/lnctl/resources.go) 保持一致。

## 常见用途

- 查看 `Ingress` / `Gateway` / DNS CRD 监听后是否正确落到主库
- 查看某个 hostname 是否写成了正确的 `DomainEndpoint` 和 `HTTPRoute`
- 查看证书 revision 是否写入
- 查看 replication changelog 是否产生

## 默认地址

```bash
http://127.0.0.1:8080
```

可以通过参数或环境变量覆盖：

```bash
lnctl --master http://<master-host>:8080 domain_endpoints ls
export LUNA_MASTER_MANAGE_URL=http://<master-host>:8080
```

## 示例

```bash
lnctl domain_endpoints ls
lnctl domain_endpoints get k8s:domain:app.example.com
lnctl http_routes ls
lnctl certificate_revisions ls
lnctl snapshot_records ls
```

## 排查顺序

1. 先看 `domain_endpoints`
2. 再看 `http_routes` / `service_backend_refs`
3. 再看 `certificate_revisions`
4. 最后看 `snapshot_records`

如果主库里都没有，问题在 master 侧物化或副作用，不在 slave。

## 存在的问题

- 资源列表需要和 manage API 人工同步
- 目前只做 CRUD 调试，还没有更高层的诊断命令
