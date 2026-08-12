Split into 4 layers with fixed responsibilities:

```
CRD (api/v1beta1)
    ↓
validation/          → reject invalid specs (already well placed)
    ↓
resources/           → PURE builders (desired K8s objects, zero client)
    ↓
reconcile/           → business logic + apply (client, retry, status)
    ↓
controller/          → thin orchestration: Reconcile() = step pipeline
    ↓
neo4j/               → Bolt client + Cypher (already separate, to subdivide)
```

**Golden rule**: `Reconcile()` must not exceed ~150 lines — only a chain of named steps.
