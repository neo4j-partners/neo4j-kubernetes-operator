# Operator logs

How to read and tune the operator's own log. This is not Neo4j's log — for that, see
[Configuration](../03-neo4j/06-configuration.md#neo4j-logging) and `kubectl logs` on a Neo4j pod.

## Levels

| Level | Zap / flag | What you see |
|-------|------------|--------------|
| error | `--zap-log-level=error` | Failures only |
| info | `--zap-log-level=info` (default, production) | State changes: apply create/update, step requeue, delete |
| debug | `--zap-log-level=debug` or `--zap-devel` | Per-reconcile detail: step start/done, apply unchanged, conflicts |

Logger names nest as `neo4j.pipeline.<domain>` (e.g. `neo4j.pipeline.workload`). Every domain
step also sets structured keys `domain` and `reconciler` (same value: `persistence`, `trust`,
`serverconfig`, `workload`, `connectivity`, `formation`) so Apply and step logs are filterable:

```bash
kubectl logs … | jq 'select(.domain=="formation")'
kubectl logs … | jq 'select(.msg=="storage plan" or .msg=="storage observe")'
```

**Info** carries concrete actions (storage plan/PVC phase, STS reconcile, service role, ENABLE/drain,
status phase). **Debug** (`V(1)`) is per-reconcile noise (step start/done, apply unchanged, aux plans).

## Stdout vs file

- **stderr (stdout for `kubectl logs`)**: essentials — controlled by `--zap-log-level` / Helm `logging.level`.
- **Optional file**: tee with its own minimum level (often more verbose).

| Flag | Helm | Meaning |
|------|------|---------|
| `--zap-log-level` | `logging.level` | stderr verbosity (`debug` \| `info` \| `error`) |
| `--zap-devel` | `logging.devel` | Console encoder + debug defaults |
| `--log-file` | `logging.file.enabled` + path | Tee path inside the pod |
| `--log-file-level` | `logging.file.level` | File verbosity (default `debug`) |

### Helm example — essentials on logs, verbose file

```yaml
logging:
  level: info
  devel: false
  file:
    enabled: true
    level: debug
    mountPath: /var/log/neo4j-operator
    filename: operator.log
```

```bash
kubectl logs -n neo4j-operator-system deploy/neo4j-operator-controller-manager

# Manager image is distroless (no sh/cat). Use an ephemeral debug container that
# shares the pod volumes to read the tee file:
POD=$(kubectl get pod -n neo4j-operator-system -l app.kubernetes.io/name=neo4j-operator -o jsonpath='{.items[0].metadata.name}')
kubectl debug -n neo4j-operator-system -it "$POD" --image=busybox:1.36 --target=manager -- \
  cat /var/log/neo4j-operator/operator.log
```

File storage is an `emptyDir` (lost on pod restart). Mount a PVC later if you need retention.
`kubectl exec … -- sh` will fail: there is no shell in the manager image.

### Local run

```bash
go run ./src/cmd/manager/main.go --leader-elect=false \
  --zap-devel --zap-log-level=debug \
  --log-file=/tmp/neo4j-operator.log --log-file-level=debug
```

## Errors

The log explains *how* a reconcile failed; the resource explains *what* failed, with a stable reason
you can look up in the [error reference](../05-reference/errors.md). Start from the resource, then come
here for detail:

```bash
kubectl describe neo4j <name> -n <namespace>
kubectl logs -n neo4j-operator-system deploy/neo4j-operator-controller-manager --tail=200
```

Symptom-driven fixes: [Common issues](01-common-issues.md).
