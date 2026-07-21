# Neo4j Operator — Examples

Working `Neo4j` CR manifests for every field the operator actually wires up today. Every example
uses `edition: enterprise`, `version: "2026.05.0"`, `license.accept: "yes"`, and only sets fields
that are read by the reconciler/render code (see [What is NOT demonstrated yet](#what-is-not-demonstrated-yet)
for schema fields that are currently no-ops).

## How to use

1. **Install the operator** first — see
   [`docs/03-user-documentation/operator/02-installation.md`](../docs/03-user-documentation/operator/02-installation.md)
   (or a platform quickstart: [kind](../docs/03-user-documentation/quickstart/local-kind/install.md),
   [Azure AKS](../docs/03-user-documentation/quickstart/azure-aks/install.md)).
2. **Pick a namespace.** Every example omits `metadata.namespace`, so `kubectl apply` targets your
   current context's default namespace. Add `-n <namespace>` to `kubectl apply`/`kubectl create`
   calls, or set `metadata.namespace` in the CR, to use another one.
3. **Check prerequisites.** Each file's header comment lists what to create first (Secrets, TLS
   material, StorageClass, tainted nodes, …) — most examples need nothing beyond the operator.
4. **Apply and watch:**

   ```bash
   kubectl apply -f examples/standalone/01-minimal.yaml
   kubectl get neo4j dev-minimal -w
   ```

Every `metadata.name` across this tree is unique, so you can `kubectl apply -f examples/standalone/`
or `-f examples/cluster/` recursively to stand up many examples side by side in one namespace
(mind resource/PVC usage — each is a full StatefulSet with persistent storage).

## Standalone

| File | Demonstrates |
|------|--------------|
| [`standalone/01-minimal.yaml`](standalone/01-minimal.yaml) | Smallest valid Standalone CR |
| [`standalone/02-auth-existing-secret.yaml`](standalone/02-auth-existing-secret.yaml) | `auth.passwordSecretRef` against an existing Secret |
| [`standalone/03-storage-storageclass.yaml`](standalone/03-storage-storageclass.yaml) | `storage.volumes.data.dynamic.storageClassName` (AKS `managed-csi`) |
| [`standalone/04-service-clusterip.yaml`](standalone/04-service-clusterip.yaml) | Explicit `connectivity.service.type: ClusterIP` + `expose` |
| [`standalone/05-service-loadbalancer.yaml`](standalone/05-service-loadbalancer.yaml) | `connectivity.service.type: LoadBalancer` |
| [`standalone/06-service-nodeport.yaml`](standalone/06-service-nodeport.yaml) | `connectivity.service.type: NodePort` |
| [`standalone/07-tls-https-bolt.yaml`](standalone/07-tls-https-bolt.yaml) | HTTPS + Bolt TLS (Browser over `https+bolt+s`) |
| [`standalone/08-tls-bolt-only.yaml`](standalone/08-tls-bolt-only.yaml) | Bolt-only TLS, no HTTPS listener |
| [`standalone/09-listeners-backup-metrics.yaml`](standalone/09-listeners-backup-metrics.yaml) | `connectivity.listeners.backup`/`metrics` + `features.backup`/`monitoring.prometheus` |
| [`standalone/10-scheduling.yaml`](standalone/10-scheduling.yaml) | `nodeSelector`, `tolerations`, soft pod anti-affinity, topology spread |
| [`standalone/11-probes-custom.yaml`](standalone/11-probes-custom.yaml) | Custom `spec.probes` (startup/readiness/liveness) |
| [`standalone/12-config-jvm.yaml`](standalone/12-config-jvm.yaml) | `config.neo4j`, `config.jvm`, `config.apoc` |
| [`standalone/13-plugins-apoc.yaml`](standalone/13-plugins-apoc.yaml) | `spec.plugins` (Standalone-only) + `pluginDefinitions` |
| [`standalone/14-image-pullsecrets.yaml`](standalone/14-image-pullsecrets.yaml) | `image.repository`/`pullPolicy`/`pullSecrets` |
| [`standalone/15-full.yaml`](standalone/15-full.yaml) | Kitchen sink — everything above combined (`dev-full`) |

## Cluster

| File | Demonstrates |
|------|--------------|
| [`cluster/01-minimal-3-primaries.yaml`](cluster/01-minimal-3-primaries.yaml) | Minimal real HA Cluster — 3 primaries |
| [`cluster/02-lab-single-primary.yaml`](cluster/02-lab-single-primary.yaml) | 1 primary — lab only, **not HA** |
| [`cluster/03-pools-analytics-read.yaml`](cluster/03-pools-analytics-read.yaml) | `secondaries.analytics` (GDS+Bloom) + `secondaries.read` (APOC) |
| [`cluster/04-service-loadbalancer.yaml`](cluster/04-service-loadbalancer.yaml) | `connectivity.service.type: LoadBalancer` |
| [`cluster/05-service-nodeport.yaml`](cluster/05-service-nodeport.yaml) | `connectivity.service.type: NodePort` |
| [`cluster/06-tls-full.yaml`](cluster/06-tls-full.yaml) | Cluster mTLS + HTTPS + Bolt TLS + backup (`prod-tls`) |
| [`cluster/07-tls-cluster-only.yaml`](cluster/07-tls-cluster-only.yaml) | Cluster mTLS only, no client-facing TLS (`prod-mtls`) |
| [`cluster/08-scheduling.yaml`](cluster/08-scheduling.yaml) | Hard pod anti-affinity, tolerations, topology spread |
| [`cluster/09-probes-custom.yaml`](cluster/09-probes-custom.yaml) | Custom probes tuned for cluster formation |
| [`cluster/10-config-jvm.yaml`](cluster/10-config-jvm.yaml) | `config.neo4j`, `config.jvm`, `config.apoc` |
| [`cluster/11-plugins-apoc.yaml`](cluster/11-plugins-apoc.yaml) | `topology.primaries.plugins` (APOC only — GDS/Bloom forbidden on primaries) |
| [`cluster/12-backup-and-metrics.yaml`](cluster/12-backup-and-metrics.yaml) | Backup + Prometheus metrics listeners |
| [`cluster/13-scale-out.yaml`](cluster/13-scale-out.yaml) | `primaries.members` scale-out — **manual `ENABLE SERVER` required**, see file |
| [`cluster/14-full.yaml`](cluster/14-full.yaml) | Kitchen sink — everything above combined (`prod-full`) |

## Feature × topology matrix

| Feature | Standalone | Cluster |
|---------|------------|---------|
| Minimal deploy | [`standalone/01`](standalone/01-minimal.yaml) | [`cluster/01`](cluster/01-minimal-3-primaries.yaml) |
| Existing auth Secret | [`standalone/02`](standalone/02-auth-existing-secret.yaml) | *(same field, not re-demonstrated)* |
| StorageClass | [`standalone/03`](standalone/03-storage-storageclass.yaml) | [`cluster/14`](cluster/14-full.yaml) |
| Service: ClusterIP | [`standalone/04`](standalone/04-service-clusterip.yaml) | [`cluster/01`](cluster/01-minimal-3-primaries.yaml) |
| Service: LoadBalancer | [`standalone/05`](standalone/05-service-loadbalancer.yaml) | [`cluster/04`](cluster/04-service-loadbalancer.yaml) |
| Service: NodePort | [`standalone/06`](standalone/06-service-nodeport.yaml) | [`cluster/05`](cluster/05-service-nodeport.yaml) |
| Bolt + HTTPS TLS | [`standalone/07`](standalone/07-tls-https-bolt.yaml) | [`cluster/06`](cluster/06-tls-full.yaml) |
| Bolt-only TLS | [`standalone/08`](standalone/08-tls-bolt-only.yaml) | *(bolt TLS bundled in `cluster/06`)* |
| Cluster mTLS | n/a (Standalone has no cluster policy) | [`cluster/06`](cluster/06-tls-full.yaml), [`cluster/07`](cluster/07-tls-cluster-only.yaml) |
| Backup listener/feature | [`standalone/09`](standalone/09-listeners-backup-metrics.yaml) | [`cluster/12`](cluster/12-backup-and-metrics.yaml) |
| Prometheus metrics listener/feature | [`standalone/09`](standalone/09-listeners-backup-metrics.yaml) | [`cluster/12`](cluster/12-backup-and-metrics.yaml) |
| Scheduling (affinity/tolerations/spread) | [`standalone/10`](standalone/10-scheduling.yaml) | [`cluster/08`](cluster/08-scheduling.yaml) |
| Custom probes | [`standalone/11`](standalone/11-probes-custom.yaml) | [`cluster/09`](cluster/09-probes-custom.yaml) |
| `config.neo4j` / `config.jvm` / `config.apoc` | [`standalone/12`](standalone/12-config-jvm.yaml) | [`cluster/10`](cluster/10-config-jvm.yaml) |
| Plugins — APOC | [`standalone/13`](standalone/13-plugins-apoc.yaml) (`spec.plugins`) | [`cluster/11`](cluster/11-plugins-apoc.yaml) (`primaries.plugins`) |
| Plugins — GDS / Bloom | n/a (Standalone `spec.plugins` also accepts gds/bloom, untested combo here) | [`cluster/03`](cluster/03-pools-analytics-read.yaml) (`secondaries.analytics.plugins` only) |
| Image repository / pullPolicy / pullSecrets | [`standalone/14`](standalone/14-image-pullsecrets.yaml) | *(same fields, not re-demonstrated)* |
| Scale-out | n/a (single pool) | [`cluster/13`](cluster/13-scale-out.yaml) |
| Kitchen sink | [`standalone/15`](standalone/15-full.yaml) | [`cluster/14`](cluster/14-full.yaml) |

## TLS / auth / plugin prerequisites

- **Auth:** `generatePassword: true` (default across most examples) needs nothing extra. Using
  `auth.passwordSecretRef` needs the Secret applied first — see
  [`secrets/auth-password.yaml`](secrets/auth-password.yaml).
- **TLS:** generated on demand with `./hack/gen-cluster-tls.sh <namespace> <name> <primary-count>`.
  Full walkthrough, `EXTRA_DNS` for LoadBalancer/Browser HTTPS, and `bolt+s://` vs `neo4j+s://`
  notes: [`secrets/README.md`](secrets/README.md).
- **Plugins:** APOC needs no license. GDS/Bloom Enterprise features need a real license file
  referenced via `pluginDefinitions.{gds,bloom}.licenseSecretRef` — placeholder Secrets (with a
  dummy `REPLACE_ME` value) are in [`secrets/plugin-licenses.yaml`](secrets/plugin-licenses.yaml).
  GDS and Bloom may only be installed on `secondaries.analytics` in Cluster mode (CRD-enforced);
  APOC may go on primaries, analytics, read, or (Standalone) `spec.plugins`.
- **Primary count parity:** `topology.primaries.members` must be odd (quorum) and
  `topology.minimumMembers` must be `<= primaries.members`.

## What is NOT demonstrated yet

These `Neo4jSpec` fields exist in the CRD schema but are **not read by any render/reconcile
code today** — setting them is accepted by the API server but has no effect on the deployed
workload. They are intentionally left out of every example above:

| Field | Status |
|-------|--------|
| `resources` | schema-only — container has no resource requests/limits set by the operator |
| `security.*` (podSecurityContext, containerSecurityContext, serviceAccount.annotations, networkPolicy) | schema-only — operator applies its own fixed security context and ServiceAccount |
| `podDisruptionBudget` | schema-only — no PDB is created |
| `maintenance.offlineMode` | schema-only |
| `podTemplate` (initContainers, sidecars, env) | schema-only |
| `storage.volumes.data.mode: Existing` / any `Share` mode | schema-only — only `Dynamic` is wired |
| `trust.certManager` | schema-only — only BYO Secret TLS (`privateKey`/`publicCertificate`) is wired |
| `connectivity.ingress.enabled: true` | schema-only — no Ingress object is created |
| `connectivity.reverseProxy` | schema-only |
| `connectivity.multiCluster.enabled: true` | rejected by CRD validation in V1 |
| `auth.ldap` | schema-only |
| `features.monitoring.csv` / `.jmx` / `.graphite` / `.serviceMonitor` | schema-only — only `features.monitoring.prometheus` (listener) is wired |
| Automatic `ENABLE SERVER` on scale-out/in | not implemented — see the big comment in [`cluster/13-scale-out.yaml`](cluster/13-scale-out.yaml) (tracked as `NEO-3-011-SRV-01`) |
| Rolling Neo4j version upgrade | deferred — `spec.version` is applied at install time only |

## Quick test commands

```bash
# Apply and watch
kubectl apply -f examples/standalone/01-minimal.yaml
kubectl get neo4j dev-minimal -w
kubectl get pods -l app.kubernetes.io/instance=dev-minimal

# Status
kubectl get neo4j dev-minimal -o jsonpath='{range .status.conditions[*]}{.type}={.status} ({.reason}){"\n"}{end}'

# Credentials (generatePassword: true → Secret "<name>-auth")
kubectl get secret dev-minimal-auth -o jsonpath='{.data.NEO4J_AUTH}' | base64 -d && echo

# Port-forward + cypher-shell
kubectl port-forward svc/dev-minimal 7687:7687
cypher-shell -a neo4j://localhost:7687 -u neo4j -p <password-from-secret>

# Browser over HTTPS (bolt+s) — needs a TLS example, e.g. standalone/07-tls-https-bolt.yaml
kubectl port-forward svc/dev-tls-https 7473:7473 7687:7687
# Connect with bolt+s://127.0.0.1:7687 — do NOT use neo4j+s over port-forward
# (routing returns in-cluster DNS that your laptop cannot reach).

# Clean up
kubectl delete -f examples/standalone/01-minimal.yaml
```
