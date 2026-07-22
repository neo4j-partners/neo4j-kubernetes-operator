# Neo4j Kubernetes Operator

Kubernetes operator that deploys and manages **Neo4j Enterprise** as a single Custom Resource (`Neo4j`).

```text
module: github.com/neo4j/neo4j-kubernetes-operator
API:    neo4j.com/v1beta1
Kind:   Neo4j
```

Supports **Standalone** and **Cluster** topologies, BYO TLS, Service exposure (ClusterIP / NodePort / LoadBalancer), scheduling, probes, config/JVM, plugins (APOC / GDS / Bloom), and status conditions.

---

## Contents

- [Quick start](#quick-start)
- [Install the operator](#install-the-operator)
- [Apply a workload](#apply-a-workload)
- [Status & connection](#status--connection)
- [Examples](#examples)
- [Feature matrix](#feature-matrix)
- [TLS lab helpers](#tls-lab-helpers)
- [Documentation](#documentation)
- [Development](#development)
- [Not yet implemented](#not-yet-implemented)

---



## Quick start

```bash
# 1. Install CRD + operator (requires kubeconfig; image via IMG=…)
make deploy IMG=controller:latest

# 2. Deploy a Standalone database
kubectl apply -f examples/standalone/01-minimal.yaml
kubectl get neo4j dev-minimal -w

# 3. Get password and connect
kubectl get secret dev-minimal-auth -o jsonpath='{.data.NEO4J_AUTH}' | base64 -d; echo
kubectl port-forward svc/dev-minimal 7687:7687 7474:7474
```

Platform guides:

- [Local kind](docs/03-user-documentation/quickstart/local-kind/install.md)
- [Azure AKS](docs/03-user-documentation/quickstart/azure-aks/install.md)
- [Operator install](docs/03-user-documentation/operator/02-installation.md)

More apply-ready manifests: `[examples/README.md](examples/README.md)`.

---



## Install the operator

```bash
# Build / push your image, then:
export IMG=<registry>/neo4j-kubernetes-operator:<tag>
make docker-build
make deploy IMG=$IMG
```

Useful Make targets:


| Target          | Action                                          |
| --------------- | ----------------------------------------------- |
| `make install`  | Apply CRDs                                      |
| `make deploy`   | CRDs + RBAC + manager Deployment                |
| `make undeploy` | Remove operator (keeps CRDs / Neo4j data)       |
| `make run`      | Run controller locally (`--leader-elect=false`) |
| `make test`     | Unit tests                                      |


Default watch scope is **single namespace** (see operator install docs). The manager Deployment includes a default toleration for `dedicated=neo4j:NoSchedule` so the operator can schedule on tainted AKS node pools used for Neo4j.

---



## Apply a workload

```bash
# Standalone
kubectl apply -f examples/standalone/01-minimal.yaml

# Cluster (3 primaries)
kubectl apply -f examples/cluster/01-minimal-3-primaries.yaml

# With existing password
kubectl apply -f examples/secrets/auth-password.yaml
kubectl apply -f examples/standalone/02-auth-existing-secret.yaml
```

Watch readiness:

```bash
kubectl get neo4j
kubectl describe neo4j <name>
kubectl get neo4j <name> -o jsonpath='{range .status.conditions[*]}{.type}={.status} ({.reason}){"\n"}{end}'
```

---



## Status & connection

Typical conditions: `Ready`, `Reconciling`, `Installed`, `Error`, `StorageReady` (and TLS-related signals when trust is enabled).

```bash
# In-cluster URIs (when populated)
kubectl get neo4j <name> -o jsonpath='{.status.endpoints}{"\n"}'

# Password Secret name is usually "<name>-auth" when generatePassword: true
kubectl get secret <name>-auth -o jsonpath='{.data.NEO4J_AUTH}' | base64 -d; echo
```

---



## Examples

Full catalog of apply-ready manifests (Standalone, Cluster, storage, secrets, TLS helpers):

**→** `[examples/README.md](examples/README.md)`

Also under `[config/samples/](config/samples/)` for kubebuilder scaffolding (subset of the examples above).

---



## Feature matrix


| Feature                                        | Standalone | Cluster |
| ---------------------------------------------- | ---------- | ------- |
| Deploy                                         | yes        | yes     |
| Generated / existing auth Secret               | yes        | yes     |
| Dynamic / Existing data + aux volumes          | yes        | yes*    |
| ClusterIP / NodePort / LoadBalancer            | yes        | yes     |
| HTTP + Bolt / HTTPS + Bolt TLS                 | yes        | yes     |
| Cluster mTLS                                   | —          | yes     |
| Backup / Prometheus listeners + ServiceMonitor | yes        | yes     |
| Scheduling / custom probes                     | yes        | yes     |
| Config / JVM / APOC conf                       | yes        | yes     |
| Plugins (APOC / GDS / Bloom)                   | yes        | yes†    |
| Status conditions / endpoints                  | yes        | yes     |


Existing `claimName` is Standalone-oriented (single RWO PVC); prefer Dynamic or `volumeClaimTemplate` for Cluster.  
†GDS / Bloom on Cluster: analytics pool only.

See `[examples/README.md](examples/README.md)` for per-feature manifests and schema-only fields.

---



## TLS lab helpers

```bash
# Standalone HTTPS + Bolt (matches examples/standalone/07-tls-https-bolt.yaml)
./hack/gen-cluster-tls.sh default dev-tls-https 1
kubectl apply -f examples/standalone/07-tls-https-bolt.yaml

# Cluster full TLS (matches examples/cluster/06-tls-full.yaml)
EXTRA_DNS=neo4j.localhost ./hack/gen-cluster-tls.sh default prod-tls 3
kubectl apply -f examples/cluster/06-tls-full.yaml
```

**Browser over port-forward:** use `bolt+s://127.0.0.1:7687`, not `neo4j+s://` (routing returns in-cluster DNS). Details: `[examples/secrets/README.md](examples/secrets/README.md)`.

---



## Documentation


| Area                  | Path                                                                                                                           |
| --------------------- | ------------------------------------------------------------------------------------------------------------------------------ |
| User docs index       | `[docs/03-user-documentation/readme.md](docs/03-user-documentation/readme.md)`                                                 |
| Standalone quickstart | `[docs/03-user-documentation/neo4j/01-quickstart-standalone.md](docs/03-user-documentation/neo4j/01-quickstart-standalone.md)` |
| Cluster quickstart    | `[docs/03-user-documentation/neo4j/02-quickstart-cluster.md](docs/03-user-documentation/neo4j/02-quickstart-cluster.md)`       |
| API cheatsheet        | `[docs/03-user-documentation/reference/api-cheatsheet.md](docs/03-user-documentation/reference/api-cheatsheet.md)`             |
| CRD spec              | `[docs/02-technical-design/crd-spec/](docs/02-technical-design/crd-spec/)`                                                     |
| Decision records      | `[docs/02-technical-design/decision-records/readme.md](docs/02-technical-design/decision-records/readme.md)`                   |
| PRD / FRs             | `[docs/01-prd/](docs/01-prd/)`                                                                                                 |


---



## Development

```bash
go test ./src/...
make generate manifests
make run   # against current kubeconfig
```

Go module: `github.com/neo4j/neo4j-kubernetes-operator`.

Layout:

```text
src/api/v1beta1/          CRD types
src/cmd/manager/          entrypoint
src/internal/controller/  reconcile pipeline
src/internal/domain/      persistence, trust, config, workload, connectivity
src/internal/render/      pure object builders
src/internal/status/      conditions / endpoints
config/                   CRD, RBAC, manager manifests
examples/                 apply-ready test manifests
hack/                     lab helpers (TLS)
```

---



## Not yet implemented

Tracked product gaps / schema fields that are **not** wired (do not rely on them in tests):


| Item                               | Notes                                                                                                                                        |
| ---------------------------------- | -------------------------------------------------------------------------------------------------------------------------------------------- |
| **Automatic** `ENABLE SERVER`      | STS scale works; DBMS enablement is manual — `[examples/cluster/13-scale-out.yaml](examples/cluster/13-scale-out.yaml)` (`NEO-3-011-SRV-01`) |
| `resources`                        | No requests/limits from CR                                                                                                                   |
| `security.*`                       | SA annotations / securityContext / NetworkPolicy not applied from CR                                                                         |
| PDB, Ingress, cert-manager         | Deferred                                                                                                                                     |
| CSV / JMX / Graphite monitoring    | Deferred (`features.monitoring.prometheus` + `serviceMonitor` are wired)                                                                     |
| LDAP, reverse proxy, multi-cluster | Deferred                                                                                                                                     |
| Neo4j version upgrade workflow     | Deferred                                                                                                                                     |


