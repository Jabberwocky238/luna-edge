# Luna Edge

[中文说明](./README.zh-CN.md)

Luna Edge is a unified edge control plane for DNS, HTTP ingress, TLS, certificate issuance, and certificate distribution.

## Current Architecture

- `master` is the single writer.
- `master` stores desired state in SQLite or Postgres.
- `master` watches Kubernetes resources through `engine/master/k8s_bridge`.
- `master` materializes them into repository rows, triggers certificate side effects, then publishes replication changelogs.
- `slave` keeps a local SQLite cache plus certificate files on disk.
- `slave` serves DNS and ingress only from local state.

## Replication Model

Replication is now split into two explicit paths:

- `Subscribe` is the primary realtime path.
  It delivers one `ChangeNotification` per materialized DNS or domain change.
- `GetSnapshot` is the recovery path.
  It is used for initial catch-up or when a slave detects a gap in `snapshot_record_id`.

Normal steady-state updates should not abuse full snapshot rebuilds.

Current payload model:

- `DNSRecord` carries `deleted`
- `DomainEntryProjection` carries `deleted`
- slave applies these changes directly to local cache rows

## Certificate Flow

- `master` decides whether a hostname needs a certificate through the cert reconciler.
- ACME http-01 challenge data is served directly by master HTTP endpoints.
- Issued certificate metadata is stored in the main repository.
- Actual bundle bytes are fetched by slaves through replication RPC `FetchCertificateBundle`.
- slave writes `tls.crt`, `tls.key`, and `metadata.json` under its local certificate root.

## Kubernetes Flow

`master` owns Kubernetes listening.

- `engine/master/k8s_bridge/dns.go` watches DNS CRDs and writes `DNSRecord`
- `engine/master/k8s_bridge/ingress.go` watches `Ingress` and writes `DomainEndpoint`, `HTTPRoute`, and `ServiceBackendRef`
- `engine/master/k8s_bridge/gateway*.go` watches Gateway API resources and writes the same materialized model

The write order is:

1. write master database
2. trigger side effects such as certificate reconciliation
3. publish replication changelog

## Main Directories

- `cmd/master`: master binary entrypoint
- `cmd/slave`: slave binary entrypoint
- `cmd/lnctl`: CLI wrapper around the manage API
- `engine/master`: control-plane runtime
- `engine/slave`: slave runtime and local store
- `dns`: authoritative DNS runtime
- `ingress`: HTTP/TLS runtime
- `replication`: protobuf and generated RPC bindings
- `repository`: storage interfaces, models, and Gorm implementations
- `deploy`: Kubernetes manifests

## Status Notes

- architecture is in active migration
- compatibility is not the priority
- read the module `README.md` files for the current responsibilities and known issues

## Development

```bash
go test ./...
```

## License

MIT. See [LICENSE](./LICENSE).
