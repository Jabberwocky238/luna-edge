# Luna Edge

[中文说明](./README.zh-CN.md)
1
Luna Edge is a unified edge system for domain-facing traffic. It puts DNS, ingress, TLS, certificate distribution, and node activation behind one control model instead of treating them as unrelated products.

## What It Does

For one hostname, Luna Edge can manage:

- DNS publishing
- traffic routing
- upstream service binding
- TLS termination
- certificate issuance, renewal, and distribution
- node-level activation

## Core Idea

Luna Edge models one domain entry as one coherent unit.

The `master` owns desired state and computes the materialized view each `slave` should run. A `slave` does not care about intermediate changes. It only cares about converging to the latest final state as fast as possible.

That is why replication is version-based, not replay-based:

- each node tracks a `VersionVector`
- subscription is kept for realtime push
- actual recovery and reconciliation use `GetSnapshot`
- there is no event log or cursor replay path in the active design

## Architecture

### Control Plane

- `master` accepts writes
- `master` stores metadata in SQLite or Postgres
- `master` builds per-node snapshots from repository projections
- `master` pushes lightweight change notifications to subscribed slaves
- `master` serves certificate bundle download through `FetchCertificateBundle`

### Data Plane

- `slave` keeps a local materialized store
- `slave` subscribes to the master with its current `VersionVector`
- when master reports a newer version, `slave` fetches the latest snapshot and replaces local state
- `slave` runs DNS and ingress from local state
- `slave` pulls certificate files from `master`, writes them to local cert root, and serves them locally

### Replication Model

Replication is built around final state convergence:

1. `slave` reads its local versions.
2. `slave` opens `Subscribe(node_id, known_versions)`.
3. `master` compares versions and pushes a `ChangeNotification` when the node should refresh.
4. `slave` calls `GetSnapshot(node_id)`.
5. `slave` replaces local snapshot state in one transaction.
6. runtime components refresh from local state.

This keeps realtime push without forcing slaves to replay every intermediate mutation.

### Certificate Flow

- `master` keeps certificate metadata in the repository
- actual `tls.crt`, `tls.key`, and `metadata.json` are fetched through replication RPC
- `slave` certificate sync is handled by `engine/slave/CertManager`
- ingress loads certificates from local disk only

### TLS Resolver

The ingress TLS resolver is intentionally strict:

- hostname input is sanitized before mapping to filesystem paths
- certificate root must be a valid non-empty directory
- filesystem watchers are used only to invalidate affected cache entries
- watchers do not preload certificates and do not clear the whole cache
- watcher implementation is split for Windows and non-Windows

## Components

- `cmd/master`: master binary
- `cmd/slave`: slave binary
- `engine/master`: control plane engine and replication service
- `engine/slave`: replica engine, local store, cert manager
- `dns/`: authoritative DNS runtime
- `ingress/`: HTTP/TLS ingress runtime
- `replication/`: protobuf definitions and generated gRPC bindings
- `repository/`: metadata repositories and storage abstraction
- `lnctl/`: control helpers and client utilities

## Storage

- `master`: SQLite for local/single-node setups, Postgres for centralized control plane
- `slave`: SQLite for local materialized metadata
- certificate payloads are stored outside the metadata DB and synced as files

## Current Runtime Assumptions

- `master` is the single writer
- `slave` is read-only from the perspective of desired state
- subscription is for latency, snapshot is the source of truth
- local runtime should continue to work from local state during transient master disconnects

## Development

Run all tests with:

```bash
go test ./...
```

## License

MIT. See [LICENSE](./LICENSE).
