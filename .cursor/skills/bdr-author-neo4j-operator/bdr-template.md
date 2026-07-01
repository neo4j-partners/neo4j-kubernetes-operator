# BDR template (business decision)

Copy to `design/decision-records/business/NNN-short-title.md`.  
Next free ID: check `design/decision-records/readme.md` index.

```markdown
# BDR-NNN — [Title]

| | |
|---|---|
| **Status** | proposed |
| **Date** | YYYY-MM-DD |
| **Depends on** | [BDR-00x](...) |
| **Helm scope** | `helm_path` rows, `AGG-*` group |
| **Constraints** | FR IDs, Neo4j docs |

---

## Context

[Forces at play — no judgement]

---

## Plugin invariants / cross-cutting rules (if applicable)

| Rule | Rationale |
|------|-----------|
| | |

---

## Options under review

### Option A — [name]

```yaml
# sketch
```

| Advantages | Disadvantages |
|------------|---------------|
| | |

### Option B — [name]

...

---

## Comparison

| Criterion | A | B | C |
|-----------|---|---|---|
| Helm parity | | | |
| API minimalism | | | |
| Operator complexity | | | |
| Breaking risk | | | |

---

## Decision

**Not decided.** | **We will adopt Option X** — [one sentence].

**Proposer direction:** ...

**Recommendation:** ...

---

## Consequences

### Positive
-

### Negative
-

### Neutral
-

---

## References

- `design/analysis/helm-fields/_index.csv` — rows ...
- [Neo4j docs](...)
- [BDR-002](002-neo4j-crd-topology.md)
```

After creating: update `design/decision-records/readme.md` index.
