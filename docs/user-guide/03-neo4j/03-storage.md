# Storage

Every `Neo4j` resource needs a data volume, and that is the only storage decision you are forced to
make. Everything else — logs, metrics, backups, import, licences, plugins — has a sensible default
and becomes interesting only when you want those directories on their own disks.

Runnable manifests for every mode: [`examples/storage/`](../../../examples/storage/).

## The data volume

```yaml
spec:
  storage:
    volumes:
      data:
        mode: Dynamic
        dynamic:
          size: 100Gi
          storageClassName: premium-ssd
```

`mode` is either `Dynamic` or `Existing`; `Share` is not allowed for data. With `Dynamic`, `size` is
required, and the claim is created from the StatefulSet's volume claim template, which means one
claim per pod named `data-<statefulset>-<ordinal>`. The access mode is `ReadWriteOnce` and cannot be
changed — Neo4j stores are not shareable between servers.

Omitting `storageClassName` uses the cluster default. If there is no default, the claim stays Pending
and the instance never starts; the resource reports `StorageReady=False` with reason `PVCPending`
rather than failing, so it recovers on its own once a class becomes available.

Growing a volume is a StorageClass matter, not an operator one. Raising `size` on a class that allows
expansion propagates to the claims; on a class that does not, the change is rejected by Kubernetes.
Shrinking is never possible.

## Existing volumes

Use `Existing` when the storage already exists or must be created a specific way. Provide exactly
one of three sources.

**An existing claim**, the usual case for a single instance restored from a snapshot:

```yaml
spec:
  storage:
    volumes:
      data:
        mode: Existing
        existing:
          claimName: neo4j-data
```

**A claim template**, when you want the operator to provision one claim per pod but need full control
over the claim spec, including selectors and volume attributes:

```yaml
spec:
  storage:
    volumes:
      data:
        mode: Existing
        existing:
          volumeClaimTemplate:
            accessModes: [ReadWriteOnce]
            storageClassName: premium-ssd
            resources:
              requests:
                storage: 100Gi
```

**An inline volume**, for anything Kubernetes can express directly — a hostPath in a lab, an NFS
share, an `emptyDir` for a throwaway test:

```yaml
spec:
  storage:
    volumes:
      data:
        mode: Existing
        existing:
          volume:
            emptyDir: {}
```

For clusters, mind which of these scales. A single `claimName` is shared by name across every pod of
a pool, which is wrong for Neo4j; use `Dynamic` or a `volumeClaimTemplate` so each member gets its
own store. See [Clustering](02-clustering.md#volumes-when-an-ordinal-comes-back).

Volumes referenced with `claimName` are never deleted by the operator, whatever the retention policy
says.

## Auxiliary volumes

Neo4j writes to several directories besides `/data`. By default they live inside the data volume, and
you only declare them when you want something different:

| Volume | Mount | Typical reason to declare it |
|--------|-------|-----------------------------|
| `logs` | `/logs` | Keep logs off the data disk, or on cheaper storage |
| `metrics` | `/metrics` | Same, for CSV metrics output |
| `backups` | `/backups` | A separate, possibly larger volume for backup artefacts |
| `import` | `/import` | Share a pre-populated dataset, often read-only |
| `licenses` | `/licenses` | Plugin licence files |
| `plugins` | `/plugins` | Persist plugin jars across restarts, or import your own |

Each takes `Share`, `Dynamic` or `Existing`:

```yaml
spec:
  storage:
    volumes:
      logs:
        mode: Share
        shareFrom: data
      backups:
        mode: Dynamic
        dynamic:
          size: 200Gi
      import:
        mode: Existing
        existing:
          claimName: shared-import
```

`Share` is the interesting one: instead of a second volume, the directory becomes a subdirectory of
the data volume, per pod. It is the cheapest way to keep the layout tidy without paying for extra
claims — and it is what happens implicitly when you declare nothing. Only `shareFrom: data` is
supported.

Examples: [`06-aux-share-logs-metrics.yaml`](../../../examples/storage/06-aux-share-logs-metrics.yaml),
[`07-aux-dynamic-backups.yaml`](../../../examples/storage/07-aux-dynamic-backups.yaml),
[`08-aux-existing-import.yaml`](../../../examples/storage/08-aux-existing-import.yaml).

## Extra mounts

Two escape hatches cover what the volume fields do not.

`additionalMounts` mounts any Kubernetes volume anywhere in the container:

```yaml
spec:
  storage:
    additionalMounts:
      - name: scripts
        mountPath: /opt/scripts
        readOnly: true
        volume:
          configMap:
            name: bootstrap-scripts
```

`secretMounts` projects specific keys of a Secret as files:

```yaml
spec:
  storage:
    secretMounts:
      kerberos:
        secretName: neo4j-kerberos
        mountPath: /etc/krb5
        items:
          - key: krb5.conf
            path: krb5.conf
```

Two rules apply, and both produce a clear rejection rather than a surprise at runtime.

**Reserved paths are refused.** `/data`, `/backups`, `/config`, `/plugins`, `/logs`, `/metrics`,
`/import` and `/licenses` — and paths beneath them — belong to the operator. Mounting over them would
shadow the store or the rendered configuration. Names that collide with operator-owned volumes are
refused for the same reason.

**Secret mounts need `items`, and the Secret needs a label.** Listing keys explicitly keeps a
whole-Secret projection from leaking unrelated keys into the container, and the Secret must carry
`neo4j.com/mountable-by-operator: "true"` before the operator will mount it at all. Why that label
exists is explained in [Security](05-security.md#why-the-operator-requires-opt-in-labels); the
practical setup is in [`examples/secrets/README.md`](../../../examples/secrets/README.md).

Examples: [`09-additional-mounts.yaml`](../../../examples/storage/09-additional-mounts.yaml),
[`10-secret-mounts.yaml`](../../../examples/storage/10-secret-mounts.yaml).

## Retention

```yaml
spec:
  storage:
    volumeClaimRetention:
      whenDeleted: Retain   # default
      whenScaled: Retain    # default
```

Both default to `Retain`, so neither deleting a resource nor scaling a pool in destroys data. Opting
into `Delete` is a create-time decision: the operator pins the effective `whenDeleted` into
`status.volumeClaimRetentionWhenDeleted` on first reconcile and keeps honouring the pinned value, so
a later patch cannot silently arm data destruction. Full behaviour, including how to reclaim
retained claims:
[Uninstall](../02-operator-installation/05-uninstall.md#persistentvolumeclaim-retention).

## Checking what happened

```bash
kubectl get pvc -n default -l app.kubernetes.io/instance=dev

kubectl get neo4j dev -n default \
  -o jsonpath='{range .status.conditions[?(@.type=="StorageReady")]}{.status} {.reason}: {.message}{"\n"}{end}'
```

A Pending claim is almost always one of three things: no default StorageClass, a class name that does
not exist, or a topology mismatch where the class provisions in a zone the pod cannot be scheduled
in. `kubectl describe pvc` names which.

## Next

[Connectivity](04-connectivity.md) · [Operations](09-operations.md) ·
[Troubleshooting](../04-troubleshooting/01-common-issues.md)
