# lnctl usage

这份文档说明如何在 Go 代码里使用 `github.com/jabberwocky238/luna-edge/lnctl` 控制 master。

## 1. 创建 client

```go
package main

import lnctlkit "github.com/jabberwocky238/luna-edge/lnctl"

func main() {
	client := lnctlkit.NewClient("http://127.0.0.1:8080")
	_ = client
}
```

这里的地址应该是 master 的 manage API 基地址。

## 2. 查询 domain projection

```go
projection, err := client.QueryDomainEntryProjection("app.example.com")
if err != nil {
	panic(err)
}

fmt.Println(projection.ID)
fmt.Println(projection.Hostname)
fmt.Println(projection.BackendType)
```

对应接口：

```text
GET /manage/query/domain-entry-projection?hostname=app.example.com
```

## 3. 查询 DNS records

```go
records, err := client.QueryDNSRecords("app.example.com", "A")
if err != nil {
	panic(err)
}

for _, record := range records {
	fmt.Println(record.ID, record.FQDN, record.RecordType, record.ValuesJSON)
}
```

对应接口：

```text
GET /manage/query/dns-records?fqdn=app.example.com&record_type=A
```

## 4. 直接提交已有 plan

如果你已经自己组装好了 `lnctl.Plan`，可以直接提交：

```go
plan := &lnctlkit.Plan{
	Hostname: "app.example.com",
}

applied, err := client.ApplyPlan(plan)
if err != nil {
	panic(err)
}

fmt.Println(applied.Hostname)
```

对应接口：

```text
POST /manage/plan
```

## 5. 用 Builder 构造 plan 后再提交

这是更推荐的方式。先查当前状态，再根据目标状态构造 diff plan。

### 示例：构造一个 L7 HTTPS 入口

```go
package main

import (
	"fmt"

	lnctlkit "github.com/jabberwocky238/luna-edge/lnctl"
	"github.com/jabberwocky238/luna-edge/repository/metadata"
)

func main() {
	client := lnctlkit.NewClient("http://127.0.0.1:8080")

	existingProjection, _ := client.QueryDomainEntryProjection("app.example.com")
	existingDNSRecords, _ := client.QueryDNSRecords("app.example.com", "A")

	builder := lnctlkit.NewBuilder("app.example.com").
		WithExistingProjection(existingProjection).
		WithExistingDNSRecords(existingDNSRecords...).
		AsL7HTTPS().
		Route("/", lnctlkit.BackendTarget{
			Type:             metadata.ServiceBackendTypeSVC,
			ServiceNamespace: "default",
			ServiceName:      "frontend",
			Port:             443,
		}).
		WantDNS(metadata.DNSRecord{
			FQDN:         "app.example.com",
			RecordType:   metadata.DNSTypeA,
			RoutingClass: metadata.RoutingClassFirst,
			TTLSeconds:   60,
			ValuesJSON:   `["1.2.3.4"]`,
			Enabled:      true,
		})

	plan, err := builder.Build()
	if err != nil {
		panic(err)
	}

	applied, err := client.ApplyPlan(plan)
	if err != nil {
		panic(err)
	}

	fmt.Println(applied.Hostname)
}
```

## 6. 构造不同类型的入口

### L4 TLS passthrough

```go
plan, err := lnctlkit.NewBuilder("tcp.example.com").
	AsL4TLSPassthrough(lnctlkit.BackendTarget{
		Type:             metadata.ServiceBackendTypeSVC,
		ServiceNamespace: "default",
		ServiceName:      "stream",
		Port:             443,
	}).
	Build()
```

### L4 TLS termination

```go
plan, err := lnctlkit.NewBuilder("tcp.example.com").
	AsL4TLSTermination(lnctlkit.BackendTarget{
		Type:             metadata.ServiceBackendTypeSVC,
		ServiceNamespace: "default",
		ServiceName:      "stream",
		Port:             8443,
	}).
	Build()
```

### L7 HTTP

```go
plan, err := lnctlkit.NewBuilder("app.example.com").
	AsL7HTTP().
	Route("/", lnctlkit.BackendTarget{
		Type:             metadata.ServiceBackendTypeSVC,
		ServiceNamespace: "default",
		ServiceName:      "frontend",
		Port:             80,
	}).
	Build()
```

### L7 HTTPS

```go
plan, err := lnctlkit.NewBuilder("app.example.com").
	AsL7HTTPS().
	Route("/", lnctlkit.BackendTarget{
		Type:             metadata.ServiceBackendTypeSVC,
		ServiceNamespace: "default",
		ServiceName:      "frontend",
		Port:             443,
	}).
	Build()
```

### L7 HTTP + HTTPS

```go
plan, err := lnctlkit.NewBuilder("app.example.com").
	AsL7HTTPBoth().
	Route("/", lnctlkit.BackendTarget{
		Type:             metadata.ServiceBackendTypeSVC,
		ServiceNamespace: "default",
		ServiceName:      "frontend",
		Port:             8080,
	}).
	Build()
```

## 7. 外部后端示例

如果后端不是 Kubernetes Service，而是任意外部地址：

```go
plan, err := lnctlkit.NewBuilder("api.example.com").
	AsL7HTTPS().
	Route("/", lnctlkit.BackendTarget{
		Type:              metadata.ServiceBackendTypeExternal,
		ArbitraryEndpoint: "api.example.net",
		Port:              8443,
	}).
	Build()
```

## 8. 使用建议

- `QueryDomainEntryProjection` 在资源不存在时可能返回 master 的 `404`
- `QueryDNSRecords` 目前要求同时传 `fqdn` 和 `recordType`
- `ApplyPlan` 是直接生效操作，提交前最好先打印或审查 `Plan`
- `Builder.Build()` 生成的是 diff 结果，不是简单的目标快照

## 9. 最小完整示例

```go
package main

import (
	"encoding/json"
	"fmt"

	lnctlkit "github.com/jabberwocky238/luna-edge/lnctl"
	"github.com/jabberwocky238/luna-edge/repository/metadata"
)

func main() {
	client := lnctlkit.NewClient("http://127.0.0.1:8080")

	existingProjection, _ := client.QueryDomainEntryProjection("app.example.com")
	existingDNS, _ := client.QueryDNSRecords("app.example.com", "A")

	plan, err := lnctlkit.NewBuilder("app.example.com").
		WithExistingProjection(existingProjection).
		WithExistingDNSRecords(existingDNS...).
		AsL7HTTPS().
		Route("/", lnctlkit.BackendTarget{
			Type:             metadata.ServiceBackendTypeSVC,
			ServiceNamespace: "default",
			ServiceName:      "frontend",
			Port:             443,
		}).
		WantDNS(metadata.DNSRecord{
			FQDN:         "app.example.com",
			RecordType:   metadata.DNSTypeA,
			RoutingClass: metadata.RoutingClassFirst,
			TTLSeconds:   60,
			ValuesJSON:   `["1.2.3.4"]`,
			Enabled:      true,
		}).
		Build()
	if err != nil {
		panic(err)
	}

	body, _ := json.MarshalIndent(plan, "", "  ")
	fmt.Println(string(body))

	_, err = client.ApplyPlan(plan)
	if err != nil {
		panic(err)
	}
}
```
