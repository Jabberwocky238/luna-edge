# Luna Edge

[中文说明](./README.zh-CN.md)

Luna Edge is an edge infrastructure control plane.

It treats edge delivery as a state propagation problem: ingress behavior, DNS records, backend bindings, and certificate state are materialized once on `master`, then streamed to `slave` runtimes as incremental changes with explicit recovery semantics.

The system is aimed at platform engineering and DevOps workflows where Kubernetes-derived intent, control-plane side effects, and edge-node execution need to stay in one operational model.

## Platform Model

- `master` is the single writable control plane
- `slave` nodes execute from local state and local certificate assets
- Kubernetes resources and direct manage API writes converge into the same repository model
- replication is optimized for change streaming first, snapshot recovery second
- edge traffic serving remains local even when the control plane is remote

## What It Operates

- authoritative DNS state
- HTTP routing and TLS entry behavior
- L4 TLS passthrough and TLS termination topologies
- certificate issuance triggers and certificate bundle distribution
- edge-facing projections derived from Kubernetes resources or direct plans

## Current Capabilities

- SQLite or Postgres backing store on `master`
- manage API for direct control-plane automation
- Kubernetes bridge ingestion for DNS CRD, `Ingress`, and Gateway API resources
- control-plane side effects on `master`, including certificate reconciliation
- real-time state propagation through `Subscribe`
- recovery and re-convergence through `GetSnapshot`
- certificate asset fetch through replication RPC
- edge DNS runtime driven by replicated `DNSRecord`
- edge ingress runtime driven by replicated `DomainEntryProjection`
- Go library and CLI tooling through `lnctl`

## Operating Flow

1. Desired state enters `master` through Kubernetes bridges or the manage API.
2. `master` materializes that state into repository models.
3. Side effects run against the materialized state.
4. Incremental changes are published to `slave` nodes.
5. `slave` nodes serve traffic entirely from local state and recover through snapshot sync when needed.

## Architecture

```text
                    +-----------------------------------+
     Kubernetes Ingress / Gateway API             lnctl / manage API
                    |                                   |
                    +-----------------+-----------------+
                                      |
                                      v
                    +-----------------------------+
                    |           master            |
                    |  k8s bridge + repository    |
                    |  cert reconcile + ACME      |
                    |  changelog publisher        |
                    +-------------+---------------+
                                  |
                     Subscribe / GetSnapshot / FetchCertificateBundle
                                  |
                +-----------------+-----------------+
                |                                   |
                v                                   v
      +---------------------+             +---------------------+
      |       slave A       |             |       slave B       |
      | local sqlite cache  |             | local sqlite cache  |
      | cert files on disk  |             | cert files on disk  |
      | DNS + ingress serve |             | DNS + ingress serve |
      +---------------------+             +---------------------+
```

## Key Components

- `cmd/master`: master entrypoint
- `cmd/slave`: slave entrypoint
- `cmd/lnctl`: operator CLI for the manage API
- `lnctl`: Go client and plan builder for controlling `master`
- `engine/master`: control-plane runtime, materialization path, side effects, publish path
- `engine/slave`: local state application and edge persistence
- `dns`: edge DNS runtime
- `ingress`: edge HTTP/TLS runtime
- `replication`: streaming, recovery, and certificate fetch RPC surface
- `repository`: metadata model and persistence abstraction
- `deploy`: deployment manifests and environment helpers

## Development

```bash
go test ./...
```

For local environment bootstrap:

```bash
bash <(curl -fsSL https://raw.githubusercontent.com/jabberwocky238/luna-edge/main/deploy/prepare.sh)
bash run.sh up master
bash run.sh up slave

# optional test environments
bash run.sh up ngg # nginx k8s gateway api
bash run.sh up ngi # nginx k8s ingress
```

## License

GPL-3.0. See [LICENSE](./LICENSE).
