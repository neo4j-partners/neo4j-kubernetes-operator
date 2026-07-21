# Neo4j Kubernetes Operator

Kubernetes operator that deploys and manages **Neo4j Enterprise** as a single Custom Resource (`Neo4j`).

```text
module: github.com/neo4j/neo4j-kubernetes-operator
API:    neo4j.com/v1beta1
Kind:   Neo4j
```

Supports **Standalone** and **Cluster** topologies, BYO TLS, Service exposure (ClusterIP / NodePort / LoadBalancer), scheduling, probes, config/JVM, plugins (APOC / GDS / Bloom), and status conditions.

> **Related (different project):** [neo4j-partners/neo4j-kubernetes-operator](https://github.com/neo4j-partners/neo4j-kubernetes-operator) is a separate partners/alpha operator with different CRDs. This repository is the Neo4j product-oriented operator under `github.com/neo4j/neo4j-kubernetes-operator`.

---

## Contents

- [Quick start](#quick-start)
- [Examples (test every feature)](#examples-test-every-feature)
- [Feature matrix](#feature-matrix)
- [Install the operator](#install-the-operator)
- [Apply a workload](#apply-a-workload)
- [TLS lab helpers](#tls-lab-helpers)
- [Status & connection](#status--connection)
- [Documentation](#documentation)
- [Development](#development)
- [Not yet implemented](#not-yet-implemented)
- [License](#license)

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

---

## Examples (test every feature)

**Canonical catalog:** [`examples/README.md`](examples/README.md)

Every example is a full, apply-ready `Neo4j` manifest using only fields the operator **actually wires** today. Each file has a unique `metadata.name` so you can apply many side by side.

### Standalone

| # | Example | What it tests |
|---|---------|----------------|
| 01 | [`examples/standalone/01-minimal.yaml`](examples/standalone/01-minimal.yaml) | Smallest valid Standalone |
| 02 | [`examples/standalone/02-auth-existing-secret.yaml`](examples/standalone/02-auth-existing-secret.yaml) | Existing auth Secret (`NEO4J_AUTH`) |
| 03 | [`examples/standalone/03-storage-storageclass.yaml`](examples/standalone/03-storage-storageclass.yaml) | Dynamic PVC + StorageClass (AKS `managed-csi`) |
| 04 | [`examples/standalone/04-service-clusterip.yaml`](examples/standalone/04-service-clusterip.yaml) | ClusterIP + explicit `expose` |
| 05 | [`examples/standalone/05-service-loadbalancer.yaml`](examples/standalone/05-service-loadbalancer.yaml) | LoadBalancer |
| 06 | [`examples/standalone/06-service-nodeport.yaml`](examples/standalone/06-service-nodeport.yaml) | NodePort |
| 07 | [`examples/standalone/07-tls-https-bolt.yaml`](examples/standalone/07-tls-https-bolt.yaml) | HTTPS + Bolt TLS (Browser) |
| 08 | [`examples/standalone/08-tls-bolt-only.yaml`](examples/standalone/08-tls-bolt-only.yaml) | Bolt TLS only |
| 09 | [`examples/standalone/09-listeners-backup-metrics.yaml`](examples/standalone/09-listeners-backup-metrics.yaml) | Backup + Prometheus metrics listeners |
| 10 | [`examples/standalone/10-scheduling.yaml`](examples/standalone/10-scheduling.yaml) | nodeSelector, tolerations, anti-affinity, topology spread |
| 11 | [`examples/standalone/11-probes-custom.yaml`](examples/standalone/11-probes-custom.yaml) | Custom startup / readiness / liveness |
| 12 | [`examples/standalone/12-config-jvm.yaml`](examples/standalone/12-config-jvm.yaml) | `config.neo4j` / `jvm` / `apoc` |
| 13 | [`examples/standalone/13-plugins-apoc.yaml`](examples/standalone/13-plugins-apoc.yaml) | `spec.plugins: [apoc]` |
| 14 | [`examples/standalone/14-image-pullsecrets.yaml`](examples/standalone/14-image-pullsecrets.yaml) | Custom image + pull secrets |
| 15 | [`examples/standalone/15-full.yaml`](examples/standalone/15-full.yaml) | Kitchen sink |

### Storage

Full catalog: [`examples/storage/README.md`](examples/storage/README.md) — Dynamic / Existing data,
aux `Share`/`Dynamic`/`Existing`, `additionalMounts`, and `secretMounts` are demonstrated there.

| # | Example | What it tests |
|---|---------|----------------|
| 01 | [`examples/storage/01-dynamic-data.yaml`](examples/storage/01-dynamic-data.yaml) | Minimal Dynamic data |
| 02 | [`examples/storage/02-dynamic-storageclass.yaml`](examples/storage/02-dynamic-storageclass.yaml) | Dynamic + StorageClass + labels |
| 03 | [`examples/storage/03-existing-claimname.yaml`](examples/storage/03-existing-claimname.yaml) | Existing `claimName` (Standalone-oriented) |
| 04–05 | [`04`](examples/storage/04-existing-volume-emptydir.yaml) / [`05`](examples/storage/05-existing-volumeclaimtemplate.yaml) | Existing emptyDir / volumeClaimTemplate |
| 06–08 | [`06`](examples/storage/06-aux-share-logs-metrics.yaml)–[`08`](examples/storage/08-aux-existing-import.yaml) | Aux Share / Dynamic backups / Existing import |
| 09–10 | [`09`](examples/storage/09-additional-mounts.yaml) / [`10`](examples/storage/10-secret-mounts.yaml) | `additionalMounts` / `secretMounts` |
| 11 | [`examples/storage/11-full.yaml`](examples/storage/11-full.yaml) | Kitchen sink (`dev-storage-full`) |

### Cluster

| # | Example | What it tests |
|---|---------|----------------|
| 01 | [`examples/cluster/01-minimal-3-primaries.yaml`](examples/cluster/01-minimal-3-primaries.yaml) | HA: 3 primaries |
| 02 | [`examples/cluster/02-lab-single-primary.yaml`](examples/cluster/02-lab-single-primary.yaml) | Lab: 1 primary (**not HA**) |
| 03 | [`examples/cluster/03-pools-analytics-read.yaml`](examples/cluster/03-pools-analytics-read.yaml) | Analytics (GDS/Bloom) + read (APOC) pools |
| 04 | [`examples/cluster/04-service-loadbalancer.yaml`](examples/cluster/04-service-loadbalancer.yaml) | LoadBalancer |
| 05 | [`examples/cluster/05-service-nodeport.yaml`](examples/cluster/05-service-nodeport.yaml) | NodePort |
| 06 | [`examples/cluster/06-tls-full.yaml`](examples/cluster/06-tls-full.yaml) | Cluster mTLS + HTTPS + Bolt TLS |
| 07 | [`examples/cluster/07-tls-cluster-only.yaml`](examples/cluster/07-tls-cluster-only.yaml) | Cluster mTLS only |
| 08 | [`examples/cluster/08-scheduling.yaml`](examples/cluster/08-scheduling.yaml) | Hard anti-affinity + tolerations + spread |
| 09 | [`examples/cluster/09-probes-custom.yaml`](examples/cluster/09-probes-custom.yaml) | Custom probes (formation-friendly) |
| 10 | [`examples/cluster/10-config-jvm.yaml`](examples/cluster/10-config-jvm.yaml) | Config / JVM / APOC |
| 11 | [`examples/cluster/11-plugins-apoc.yaml`](examples/cluster/11-plugins-apoc.yaml) | APOC on primaries |
| 12 | [`examples/cluster/12-backup-and-metrics.yaml`](examples/cluster/12-backup-and-metrics.yaml) | Backup + Prometheus |
| 13 | [`examples/cluster/13-scale-out.yaml`](examples/cluster/13-scale-out.yaml) | Scale members (**manual `ENABLE SERVER`**) |
| 14 | [`examples/cluster/14-full.yaml`](examples/cluster/14-full.yaml) | Kitchen sink |

### Secrets & TLS helpers

| Path | Purpose |
|------|---------|
| [`examples/secrets/auth-password.yaml`](examples/secrets/auth-password.yaml) | Sample `NEO4J_AUTH` Secret |
| [`examples/secrets/plugin-licenses.yaml`](examples/secrets/plugin-licenses.yaml) | Placeholder GDS/Bloom license Secrets |
| [`examples/secrets/README.md`](examples/secrets/README.md) | Generate BYO TLS Secrets (`hack/gen-cluster-tls.sh`) |
| [`hack/gen-cluster-tls.sh`](hack/gen-cluster-tls.sh) | Lab TLS material for bolt / https / cluster |

Also kept under [`config/samples/`](config/samples/) for kubebuilder scaffolding (subset of the examples above).

---

## Feature matrix

| Feature | Standalone | Cluster | Example |
|---------|:----------:|:-------:|---------|
| Deploy | yes | yes | [`standalone/01`](examples/standalone/01-minimal.yaml), [`cluster/01`](examples/cluster/01-minimal-3-primaries.yaml) |
| Generated password | yes | yes | most examples |
| Existing auth Secret | yes | yes | [`standalone/02`](examples/standalone/02-auth-existing-secret.yaml) |
| Dynamic data PVC | yes | yes | all |
| StorageClass | yes | yes | [`standalone/03`](examples/standalone/03-storage-storageclass.yaml), [`storage/02`](examples/storage/02-dynamic-storageclass.yaml) |
| Existing data (`claimName` / `volume` / VCT) | yes | yes* | [`storage/03`](examples/storage/03-existing-claimname.yaml)–[`05`](examples/storage/05-existing-volumeclaimtemplate.yaml) |
| Aux Share / Dynamic / Existing | yes | yes | [`storage/06`](examples/storage/06-aux-share-logs-metrics.yaml)–[`08`](examples/storage/08-aux-existing-import.yaml) |
| `additionalMounts` / `secretMounts` | yes | yes | [`storage/09`](examples/storage/09-additional-mounts.yaml), [`storage/10`](examples/storage/10-secret-mounts.yaml) |
| ClusterIP / NodePort / LB | yes | yes | `04`–`06` / cluster `04`–`05` |
| HTTP + Bolt | yes | yes | defaults |
| HTTPS (+ requires Bolt TLS) | yes | yes | [`standalone/07`](examples/standalone/07-tls-https-bolt.yaml), [`cluster/06`](examples/cluster/06-tls-full.yaml) |
| Bolt TLS | yes | yes | [`standalone/08`](examples/standalone/08-tls-bolt-only.yaml) |
| Cluster mTLS | — | yes | [`cluster/06`](examples/cluster/06-tls-full.yaml), [`cluster/07`](examples/cluster/07-tls-cluster-only.yaml) |
| Backup listener | yes | yes | [`standalone/09`](examples/standalone/09-listeners-backup-metrics.yaml) |
| Prometheus metrics listener | yes | yes | same |
| Scheduling | yes | yes | [`standalone/10`](examples/standalone/10-scheduling.yaml), [`cluster/08`](examples/cluster/08-scheduling.yaml) |
| Custom probes | yes | yes | [`standalone/11`](examples/standalone/11-probes-custom.yaml) |
| Config / JVM / APOC conf | yes | yes | [`standalone/12`](examples/standalone/12-config-jvm.yaml) |
| Plugins APOC | `spec.plugins` | per-pool | [`standalone/13`](examples/standalone/13-plugins-apoc.yaml), [`cluster/11`](examples/cluster/11-plugins-apoc.yaml) |
| Plugins GDS / Bloom | `spec.plugins` | analytics pool only | [`cluster/03`](examples/cluster/03-pools-analytics-read.yaml) |
| Multi-pool (analytics / read) | — | yes | [`cluster/03`](examples/cluster/03-pools-analytics-read.yaml) |
| STS scale-out | — | STS yes; **ENABLE SERVER no** | [`cluster/13`](examples/cluster/13-scale-out.yaml) |
| Status conditions / endpoints | yes | yes | `kubectl get neo4j -o yaml` |

\*Existing `claimName` is Standalone-oriented (single RWO PVC); prefer Dynamic or `volumeClaimTemplate` for Cluster.

Full matrix and “schema-only / no-op” fields: [`examples/README.md`](examples/README.md).

---

## Install the operator

```bash
# Build / push your image, then:
export IMG=<registry>/neo4j-kubernetes-operator:<tag>
make docker-build
make deploy IMG=$IMG
```

Useful Make targets:

| Target | Action |
|--------|--------|
| `make install` | Apply CRDs |
| `make deploy` | CRDs + RBAC + manager Deployment |
| `make undeploy` | Remove operator (keeps CRDs / Neo4j data) |
| `make run` | Run controller locally (`--leader-elect=false`) |
| `make test` | Unit tests |

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

## TLS lab helpers

```bash
# Standalone HTTPS + Bolt (matches examples/standalone/07-tls-https-bolt.yaml)
./hack/gen-cluster-tls.sh default dev-tls-https 1
kubectl apply -f examples/standalone/07-tls-https-bolt.yaml

# Cluster full TLS (matches examples/cluster/06-tls-full.yaml)
EXTRA_DNS=neo4j.localhost ./hack/gen-cluster-tls.sh default prod-tls 3
kubectl apply -f examples/cluster/06-tls-full.yaml
```

**Browser over port-forward:** use `bolt+s://127.0.0.1:7687`, not `neo4j+s://` (routing returns in-cluster DNS). Details: [`examples/secrets/README.md`](examples/secrets/README.md).

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

## Documentation

| Area | Path |
|------|------|
| User docs index | [`docs/03-user-documentation/readme.md`](docs/03-user-documentation/readme.md) |
| Standalone quickstart | [`docs/03-user-documentation/neo4j/01-quickstart-standalone.md`](docs/03-user-documentation/neo4j/01-quickstart-standalone.md) |
| Cluster quickstart | [`docs/03-user-documentation/neo4j/02-quickstart-cluster.md`](docs/03-user-documentation/neo4j/02-quickstart-cluster.md) |
| API cheatsheet | [`docs/03-user-documentation/reference/api-cheatsheet.md`](docs/03-user-documentation/reference/api-cheatsheet.md) |
| CRD spec | [`docs/02-technical-design/crd-spec/`](docs/02-technical-design/crd-spec/) |
| Decision records | [`docs/02-technical-design/decision-records/readme.md`](docs/02-technical-design/decision-records/readme.md) |
| PRD / FRs | [`docs/01-prd/`](docs/01-prd/) |

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

| Item | Notes |
|------|--------|
| **Automatic `ENABLE SERVER`** | STS scale works; DBMS enablement is manual — [`examples/cluster/13-scale-out.yaml`](examples/cluster/13-scale-out.yaml) (`NEO-3-011-SRV-01`) |
| `resources` | No requests/limits from CR |
| `security.*` | SA annotations / securityContext / NetworkPolicy not applied from CR |
| PDB, Ingress, cert-manager | Deferred |
| LDAP, reverse proxy, multi-cluster | Deferred |
| Neo4j version upgrade workflow | Deferred |

---

## License

Apache License 2.0 — see project headers and distribution terms for Neo4j Enterprise container images (license acceptance is required on every CR via `spec.license.accept`).
