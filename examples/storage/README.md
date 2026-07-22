# Storage examples

Apply-ready manifests covering `spec.storage` — Dynamic / Existing data modes, auxiliary
volumes (`Share` / `Dynamic` / `Existing`), `additionalMounts`, and `secretMounts`.

Every CR uses `edition: enterprise`, `version: "2026.05.0"`, `license.accept: "yes"`, and
`auth.generatePassword: true`. Each `metadata.name` is unique across the whole `examples/` tree.

## Index

| File | Demonstrates |
|------|--------------|
| [`01-dynamic-data.yaml`](01-dynamic-data.yaml) | Standalone Dynamic data `10Gi` (minimal) |
| [`02-dynamic-storageclass.yaml`](02-dynamic-storageclass.yaml) | Dynamic + `storageClassName: managed-csi` + `dynamic.labels` (AKS) |
| [`03-existing-claimname-pvc.yaml`](03-existing-claimname-pvc.yaml) | Companion PVC `dev-storage-claim-data` (10Gi) |
| [`03-existing-claimname.yaml`](03-existing-claimname.yaml) | Existing `claimName` → that PVC (**Standalone-oriented**) |
| [`04-existing-volume-emptydir.yaml`](04-existing-volume-emptydir.yaml) | Existing `volume` emptyDir — lab only, not persistent |
| [`05-existing-volumeclaimtemplate.yaml`](05-existing-volumeclaimtemplate.yaml) | Existing `volumeClaimTemplate` RWO 10Gi |
| [`06-aux-share-logs-metrics.yaml`](06-aux-share-logs-metrics.yaml) | Dynamic data + logs/metrics `Share` from data |
| [`07-aux-dynamic-backups.yaml`](07-aux-dynamic-backups.yaml) | Dynamic data + backups Dynamic 20Gi + backup feature/listener |
| [`08-aux-existing-import.yaml`](08-aux-existing-import.yaml) | Dynamic data + import Existing emptyDir (lab) |
| [`09-additional-mounts.yaml`](09-additional-mounts.yaml) | Dynamic data + `additionalMounts` emptyDir at `/extra-data` |
| [`10-secret-mounts-secret.yaml`](10-secret-mounts-secret.yaml) | Companion Opaque Secret `dev-storage-secret-creds` (`config.txt`) |
| [`10-secret-mounts.yaml`](10-secret-mounts.yaml) | Dynamic data + `secretMounts` |
| [`11-full.yaml`](11-full.yaml) | Kitchen sink (`dev-storage-full`) |
| [`12-aux-share-plugins-apoc.yaml`](12-aux-share-plugins-apoc.yaml) | `volumes.plugins` Share + APOC + `config.neo4j` procedure overrides |

## How to apply

```bash
# Minimal Dynamic
kubectl apply -f examples/storage/01-dynamic-data.yaml
kubectl get neo4j dev-storage-dynamic -w

# Existing claimName — PVC first, then CR
kubectl apply -f examples/storage/03-existing-claimname-pvc.yaml
kubectl apply -f examples/storage/03-existing-claimname.yaml

# Secret mounts — Secret first, then CR
kubectl apply -f examples/storage/10-secret-mounts-secret.yaml
kubectl apply -f examples/storage/10-secret-mounts.yaml

# Or apply the whole directory (mind PVC/Secret order for 03 / 10 / 11)
kubectl apply -f examples/storage/
```

## Notes

- **Existing `claimName` is Standalone-oriented.** A single RWO PVC cannot be mounted by
  multiple Cluster members; use Dynamic or `volumeClaimTemplate` for Cluster.
- **Share** mounts reuse the data volume with `subPathExpr` such as `logs/$(POD_NAME)`
  (and `metrics/$(POD_NAME)`). `volumes.plugins` Share uses subPath `plugins` so
  `NEO4J_PLUGINS` downloads persist; see [`12-aux-share-plugins-apoc.yaml`](12-aux-share-plugins-apoc.yaml).
- **Dynamic PVC naming:** the operator creates a StatefulSet volumeClaimTemplate named `data`;
  Kubernetes names the pod-0 claim `data-<sts>-0` (Standalone STS is `<cr-name>-server`, e.g.
  `data-dev-storage-dynamic-server-0`).
- emptyDir Existing / import examples are **lab only** — data is lost when the pod is deleted.
