# cmd/lnctl

`lnctl` 是面向 master manage API 的命令行工具。

## 当前命令

- `query domain --hostname <hostname>`: 查询域名投影
- `query dns --fqdn <fqdn> --record-type <type>`: 查询 DNS 记录
- `apply <plan-name>`: 读取 `~/.lnctl/<plan-name>.json` 并提交 plan
- `build create <plan-name>`: 创建本地 plan 文件
- `build show <plan-name>`: 展示本地 plan 文件
- `build edit <plan-name>`: 用新的 JSON 内容覆盖本地 plan 文件
- `build delete <plan-name>`: 删除本地 plan 文件

## 本地 plan 存储

默认目录：

```bash
~/.lnctl
```

文件命名规则：

```bash
~/.lnctl/<plan-name>.json
```

也可以通过 `--store` 覆盖目录。

## 示例

```bash
lnctl query domain --hostname app.example.com
lnctl query dns --fqdn app.example.com --record-type A

lnctl build create app -d '{"Hostname":"app.example.com"}'
lnctl build show app
lnctl build edit app -f ./app-plan.json
lnctl apply app
lnctl build delete app
```

