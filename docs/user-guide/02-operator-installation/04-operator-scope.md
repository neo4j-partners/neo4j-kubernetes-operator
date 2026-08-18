# Watch scope and RBAC

The operator reconciles an explicit list of namespaces. There is no cluster-wide mode, and there is
no implicit default: if the scope is missing, the manager refuses to start rather than guess.

## How scope is declared

Scope is the `WATCH_NAMESPACE` environment variable on the controller container, a comma-separated
list of namespace names:

```yaml
env:
- name: WATCH_NAMESPACE
  value: "default"
- name: POD_NAMESPACE
  valueFrom:
    fieldRef:
      fieldPath: metadata.namespace
```

That is what ships in the manifests, and the Helm equivalent is `watchNamespaces`. Three values are
refused at start-up, all deliberately:

| Value | Result |
|-------|--------|
| Empty or unset | `WATCH_NAMESPACE is required` — the process exits |
| `*` | `WATCH_NAMESPACE=* (cluster-wide) is not supported` — the process exits |
| Any name equal to `POD_NAMESPACE` | Operator namespace must not be watched (NEO-016) — the process exits |

Helm also fails template render if `watchNamespaces` includes the release namespace. Install the
operator in `neo4j-operator-system` (or another dedicated namespace) and list **workload**
namespaces only. A `Neo4j` CR in the operator namespace can adopt the controller ServiceAccount.

A `Neo4j` resource created outside the watched namespaces is accepted by the API server and then
ignored: no pods, no events, no status. That silence is the usual explanation for "my resource
does nothing", so check the scope first — the operator logs it on every start:

```bash
kubectl logs -n neo4j-operator-system deploy/neo4j-operator-controller-manager | grep "watching namespaces"
```

## Scope and permissions move together

`WATCH_NAMESPACE` only tells the controller where to look. Being allowed to act there is a
separate decision, expressed as one Role and one RoleBinding per watched namespace, both bound to
the `neo4j-operator-controller-manager` ServiceAccount in `neo4j-operator-system`.

Adding a namespace therefore takes two changes, and forgetting either one produces a distinct
failure:

| You changed | Symptom |
|-------------|---------|
| Only `WATCH_NAMESPACE` | Reconcile starts and fails on the first API call, with `forbidden` errors in the log and an `Error` condition on the resource |
| Only the Role and RoleBinding | Nothing happens at all — the resource is outside the cache the controller watches |

## Adding a namespace

With Helm, list every namespace; the chart renders a Role and RoleBinding for each one and keeps
the environment variable in sync:

```bash
helm upgrade --install neo4j-operator ./charts/neo4j-operator \
  --namespace neo4j-operator-system \
  --set 'watchNamespaces={default,team-a,team-b}'
```

With the manifests, copy the existing Role and RoleBinding in `config/rbac/`, set
`metadata.namespace` on the copies, append the namespace to `WATCH_NAMESPACE` in
`config/manager/manager.yaml`, then re-apply:

```bash
kubectl apply -k config/rbac
kubectl apply -k config/manager
```

Changing the environment variable restarts the controller pod, so the new scope takes effect on the
next start rather than immediately.

## What the operator may do in a watched namespace

The Role grants full lifecycle rights on the object kinds the operator builds: ConfigMaps,
Secrets, Services, endpoints, ServiceAccounts, PersistentVolumeClaims, pods, StatefulSets, and
PodDisruptionBudgets, plus creating and patching Events, plus Roles and RoleBindings for the
per-instance ServiceAccount it can create. Leader-election leases stay in the operator namespace
on a **separate** Role; the manager Role is not granted there (NEO-016).

The consequence worth internalising is `secrets` with `get`, `list` and `watch`: **inside a watched
namespace the operator can read every Secret, not only the ones you meant for Neo4j.** Kubernetes
RBAC has no way to say "only these Secrets".

That is why the operator refuses to mount a Secret unless the Secret itself carries an opt-in
label, and why bring-your-own credentials also need to name the resource allowed to use them. The
capability cannot be narrowed, so consent is expressed on the object instead. The reasoning, the
exact labels, and what they do and do not protect against are in
[Security](../03-neo4j/05-security.md#why-the-operator-requires-opt-in-labels).

Two direct implications for how you lay out namespaces:

- Do not point the operator at a namespace holding unrelated sensitive Secrets. Give Neo4j
  workloads their own namespaces, one per team or per environment.
- Keep the watch list as short as the deployment allows. Every entry widens what a compromised
  controller could read.

## Reviewing the current scope

```bash
# Declared scope
kubectl get deployment neo4j-operator-controller-manager -n neo4j-operator-system \
  -o jsonpath='{.spec.template.spec.containers[0].env[?(@.name=="WATCH_NAMESPACE")].value}{"\n"}'

# Granted scope
kubectl get rolebinding -A \
  -o jsonpath='{range .items[?(@.metadata.name=="neo4j-operator-manager-rolebinding")]}{.metadata.namespace}{"\n"}{end}'
```

The two lists should match. When they drift, the difference tells you which of the two failure
modes above you are looking at.

## Removing a namespace

Drop it from `watchNamespaces` or `WATCH_NAMESPACE`, and delete the Role and RoleBinding there.
Existing `Neo4j` resources in that namespace keep running — the pods and Services are owned by
the resource, not by the controller — but nothing is reconciled any more: configuration changes
are not applied, failed pods are not corrected, and status goes stale. Delete or migrate those
resources before narrowing the scope.

## Next

[Uninstall the operator](05-uninstall.md) · [Your first Neo4j](../01-getting-started/first-neo4j.md)
