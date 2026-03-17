# Luna Edge

[中文说明](./README.zh-CN.md)

Luna Edge is a cloud-native multi-cluster edge convergence gateway.

It unifies DNS, HTTP ingress, TLS termination, certificate issuance, certificate distribution, and Kubernetes materialization into one control plane.

It supports Kubernetes `Ingress` and the 2026 experimental Gateway API surface, and because its repository model is deeply fused with Kubernetes materialization, it can provide a Quicksilver-like effect: control-plane desired state is written once, materialized once, and then streamed to edge nodes as small changelogs instead of rebuilding the whole world every time.

The master can be driven from both sides:

- Kubernetes resources can control master through the built-in bridge
- manage endpoints can control master directly through the built-in `lnctl` client

## Positioning

- cloud-native edge gateway
- multi-cluster control and edge distribution
- Kubernetes-native materialization model
- Kubernetes can directly control master through bridge watchers
- Gateway API experimental support for 2026 Kubernetes
- Quicksilver-like incremental propagation based on repository + replication
- fully controllable manage endpoint through `lnctl`

## Architecture

```text
                    +-----------------------------+
     Kubernetes Ingress / Gateway API              lnctl / manage API
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
## Development

```bash
bash <(curl -fsSL https://raw.githubusercontent.com/jabberwocky238/luna-edge/main/deploy/prepare.sh)

bash run.sh up master
bash run.sh up slave

# for test
bash run.sh up ngg # nginx k8s gateway api
bash run.sh up ngi # nginx k8s ingress
```

## License

See [LICENSE](./LICENSE).
