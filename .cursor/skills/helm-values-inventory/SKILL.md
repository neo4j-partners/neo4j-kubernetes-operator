---
name: helm-values-inventory
description: >-
  Inventories Helm values.yaml paths for Neo4j chart analysis. Extracts YAML keys,
  maps to Go HelmValues struct, and populates design/analysis/helm-fields/_index.csv.
  Use when starting helm field analysis or checking coverage of values.yaml.
---

# Helm values inventory

## Outputs

- `design/analysis/helm-fields/_index.csv`
- Optional: regenerate via script

## Steps

1. Read `helm-charts/neo4j/values.yaml`
2. Read `helm-charts/internal/model/release_values.go` — canonical struct names
3. Run extract script:

```bash
python design/analysis/helm-fields/scripts/extract-helm-paths.py \
  --output design/analysis/helm-fields/_index.csv
```

4. Manual pass — add missing paths:
   - Commented-only keys in values if templates reference them (`image.customImage`)
   - Sub-keys under `services.neo4j.ports.*` if exposed separately in tests

## Grouping rules

| Pattern | Treatment |
|---------|-----------|
| `config.*` | **One row** `config` — map, not per-key |
| `apoc_config`, `apoc_credentials`, `secretMounts`, `env` | One row per map root |
| `ssl.bolt`, `ssl.https`, `ssl.cluster` | Separate rows (distinct Neo4j policies) |
| `volumes.data.*` modes | Row per mode block + parent `volumes.data.mode` |

## CSV columns

Fill on inventory: `helm_path`, `domain`, `category`, `status=todo`  
Leave empty for Phase 2: `client_need`, `neo4j_doc_ref`, `helm_code_refs`, etc.

## Domain assignment

Match `helm_path` prefix to `design/analysis/helm-fields/_domains.yaml`.

## Coverage check

```bash
# Count todo rows
rg -c ',todo$' design/analysis/helm-fields/_index.csv
```

Target: 0 `todo` after Phase 2 complete.
