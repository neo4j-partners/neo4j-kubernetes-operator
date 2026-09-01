# Frequently asked questions

Short answers to the questions that come up before and during an evaluation. Each one links to the
page that covers the subject properly.

## Support

### Is the Neo4j Kubernetes Operator a supported product?

No. The operator is open-source software hosted on GitHub, and Neo4j does not support it.


### What if I hit a bug running Neo4j Enterprise Edition with the operator?

If a bug is with Enterprise Edition, Neo4j will support resolving that.

Neo4j may ask you to reproduce the issue with a supported deployment to confirm it is an EE defect.

### How do I get help with the operator itself?

**File a GitHub issue** for anything that looks like a defect or a gap in the operator. This is the
normal channel and the one that improves the product for everyone. A good report carries the `Neo4j`
resource, `kubectl get neo4j <name> -o yaml` including `status`, and the operator log — see
[Reading the operator log](04-troubleshooting/02-operator-logs.md).

**Neo4j Professional Services** can assist for a fee, which is the route when you need committed
attention on your own deployment rather than a fix in the open-source project.

Before either, check [Common issues](04-troubleshooting/01-common-issues.md): it is symptom-driven
and covers the failures that are configuration rather than defects.

## Operations

### Which changes restart my pods?

One rule decides it: **the pods roll when a change alters the rendered pod template or the rendered
`neo4j.conf`, and only then.** The operator writes the whole pod template on every update, so any
difference in it is a rollout; `neo4j.conf` reaches the pods through a checksum annotation carried
on that template. A change that renders identically to what is already there restarts nothing.

In Cluster mode the roll is one member at a time in reverse ordinal order, each waited for. On
Standalone the single pod restarts, and that is an outage. See
[Changing configuration](03-neo4j/09-operations.md#changing-configuration).

These changes roll the pods:

| Change | Why |
|---|---|
| `spec.config.*`, JVM arguments, memory settings | They render into `neo4j.conf`, which is hashed into the pod template |
| `spec.resources` | Container resources live in the pod template |
| `spec.version`, `spec.image.*` | A different image, pull policy or pull secret is a different template. Note that a version change is **not** an orchestrated upgrade — see [Version changes](03-neo4j/09-operations.md#version-changes) |
| `spec.scheduling.*` — node selector, tolerations, affinity, topology spread | They are pod-level fields |
| `spec.plugins` | Assigning or removing a plugin changes the container's environment and mounted volumes |
| Inline logging XML (`spec.logging.serverLogsXml`, `userLogsXml`) | The XML content itself is hashed |
| The **contents** of a mounted TLS Secret | Deliberately hashed into a second annotation, so a renewed certificate reaches the process |
| `kubectl rollout restart` | Your own restart; the operator preserves it rather than reverting it |

These do **not** restart anything:

| Change | Why not |
|---|---|
| Growing a data volume | The claims are patched in place; the StatefulSet's `volumeClaimTemplate` is immutable and never changes, so the pod template does not move. See [Storage](03-neo4j/03-storage.md) |
| Scaling a pool in or out, primary or secondary | Adding or removing members changes the replica count, not the template. The two settings that do track pool size are read by Neo4j only when the DBMS first initialises, so they are deliberately kept out of the checksum — hashing them would restart every member to deliver a value nothing would read, and on a single-primary cluster that is a full outage for nothing |
| The **contents** of the auth Secret | Only the Secret's name is in the pod template; the value is read through a `secretKeyRef`. Changing which Secret is referenced does roll the pods, changing what is inside it does not — and Neo4j reads the initial password only at first boot anyway |
| The **contents** of an external logging ConfigMap referenced by `serverLogsConfigMapRef` | Only the reference is hashed, not the content it points at |
| Re-applying a spec that renders identically | The checksum is unchanged, so there is nothing to roll |

To watch a rollout in progress:

```bash
kubectl rollout status statefulset/prod-primary -n default
kubectl get neo4j prod -n default -w
```

`status.observedGeneration` matching `metadata.generation` is the precise signal that your change
has been processed.