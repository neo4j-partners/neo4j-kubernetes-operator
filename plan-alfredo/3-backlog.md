# 3 — Final plan (backlog)

Non-technical. **What** we deliver in V1 and **when** — not how. The "how" is in the technical plans ([4](4-technical-shared.md)/[5](5-technical-azure.md)/[6](6-technical-aws.md)).

- **Team / time:** ~1.2 FTE (two people, 6 days/wk), **12 weeks**, **Azure first**.
- **Traceability:** every backlog item links to the customer requirements in [2-product-requirements.csv](2-product-requirements.csv) via its **Source ID** (`T####` = a row in that table, `GAP-###` = a listed gap, `NEW-###` = something we added that the customer table didn't cover).

## What's in vs out (coverage of the customer's 278 requirements)
| Bucket | # | Notes |
|---|---|---|
| **V1 — built now** | **57** | 52 from the customer table + 5 NEW |
| V1 hardening (wks 8–9) | 23 | perf/scale + resilience SLOs |
| V2 — later | 40 | backup/restore, PVC lifecycle, multi-DB, plugins, security CRDs, cert-manager |
| Out of plan | 9 | UI, sharding, fleet auto-onboarding |
| AWS follow-on | 77 | same scope, re-run on AWS adapter |
| GCP / OpenShift | 77 | deferred |

Nothing is silently dropped — every customer row has a bucket.

## The V1 backlog — 57 items in 8 build groups

### G1 — Foundation (operator core)
| ID | Source | What we need |
|---|---|---|
| V1-001 | T0077 | Helm-install the operator on AKS; controller healthy |
| V1-002 | T0078 | CRDs install and become active |
| V1-003 | T0079 | Operator restart resumes cleanly |
| V1-004 | T0082 | Namespace / watch scope correct |
| V1-005 | T0083 | RBAC grants exactly what's needed |
| V1-006 | GAP-034 | No wildcard / escalate / impersonate in permissions |
| V1-007 | GAP-035 | Leader election uses a least-privilege role |
| V1-008 | GAP-037 | Crash → reconcile resumes < 15s, no missed work |
| V1-009 | GAP-040 | Retries back off, capped at 5 min |
| V1-010 | GAP-017 | Fatal error shows up as `Degraded` |

### G2 — Standalone + configuration
| ID | Source | What we need |
|---|---|---|
| V1-011 | T0085 | Create a single Neo4j, reaches Ready |
| V1-012 | T0089 | Resource requests/limits from spec |
| V1-013 | T0088 | Safe Neo4j version/patch upgrade |
| V1-014 | T0086 | Provision + expand storage |
| V1-015 | T0092 | Config change reconciles safely |
| V1-016 | T0087 | Admin/secret change reconciles |
| V1-017 | GAP-002 | Required fields enforced + edition default |
| V1-018 | GAP-003 | Reject malformed spec |
| V1-019 | **NEW-001** | Render Neo4j config from spec |
| V1-020 | **NEW-002** | Render basic platform config (resources/scheduling/security) |

### G3 — Cluster / HA
| ID | Source | What we need |
|---|---|---|
| V1-021 | T0093 | 3-node cluster forms, Ready |
| V1-022 | T0094 | 5-node cluster forms, Ready |
| V1-023 | T0095 | Scale out, quorum preserved |
| V1-024 | T0096 | Scale in / member removal, quorum-safe |
| V1-025 | T0097 | Rolling restart stays serviceable |
| V1-026 | T0099 | Spread across zones |
| V1-027 | T0100 | Split-brain prevention |
| V1-028 | GAP-001 | HA from one manifest → Ready < 2 min |
| V1-029 | GAP-004 | Upgrade order: secondaries → non-leader → leader |
| V1-030 | GAP-016 | Reject invalid cluster size |
| V1-031 | GAP-005 | Block unsafe (store-migration) upgrades |

### G4 — Networking / ingress
| ID | Source | What we need |
|---|---|---|
| V1-032 | T0148 | Azure load-balancer integration |
| V1-033 | GAP-014 | Internal + client services healthy |
| V1-034 | GAP-015 | Optional ingress, stable Bolt sessions |

### G5 — TLS / security (user-provided certs)
| ID | Source | What we need |
|---|---|---|
| V1-035 | T0098 | TLS-enabled cluster end to end |
| V1-036 | T0114 | Cert rotation when user updates the secret |
| V1-037 | T0108 | Admin secret rotation |
| V1-038 | T0109 | Native auth works |
| V1-039 | GAP-011 | Use user-supplied TLS secret; reject plaintext |

### G6 — Azure adapter
| ID | Source | What we need |
|---|---|---|
| V1-040 | T0151 | Azure Workload/Managed Identity |
| V1-041 | T0149 | Azure managed-disk expansion |
| V1-042 | GAP-010 | Identity auth without static secrets |
| V1-043 | **NEW-003** | Azure capability detection ("detect, don't configure") |
| V1-044 | **NEW-004** | Provider-seam contract test (guards Azure↔AWS sharing) |

### G7 — Observability
| ID | Source | What we need |
|---|---|---|
| V1-045 | T0129 | Neo4j + operator metrics exposed |
| V1-046 | T0130 | Prometheus scraping works |
| V1-047 | T0131 | Health conditions on the resource |
| V1-048 | GAP-018 | Metrics series present (no UI series) |
| V1-049 | GAP-048 | Failures visible (cert/quorum → Degraded + events) |

### G8 — Day-2 / lifecycle
| ID | Source | What we need |
|---|---|---|
| V1-050 | T0080 | Operator upgrade |
| V1-051 | T0081 | Operator rollback |
| V1-052 | T0090 | Node failure recovery |
| V1-053 | T0135 | Pod crash recovery |
| V1-054 | T0136 | Node drain handled |
| V1-055 | T0140 | Neo4j process crash recovery |
| V1-056 | T0141 | Secret deletion recovery |
| V1-057 | **NEW-005** | Clean decommission/teardown, no orphans |

## The 12 weeks
| Wk | Phase | Focus | Groups |
|---|---|---|---|
| 1 | Design | CRD API, Azure seam, test harness, **real AKS account**, decide the 3 open questions | — |
| 2 | Build | Operator core | G1 |
| 3 | Build | Standalone + config | G1→G2 |
| 4 | Validation | Standalone on AKS, storage, TLS basics | G2, G5 |
| 5 | Validation | Cluster / HA | G3 |
| 6 | Validation | Networking + Azure adapter | G4, G6 |
| 7 | Validation | Observability + refinement | G7 |
| 8 | Gaps resolution | Day-2 + decommission; finish any incomplete items | G8 |
| 9 | Gaps resolution | Close gaps from validation → feature-complete | — |
| 10 | Hardening | Resilience & perf SLOs on AKS | 23 hardening items |
| 11 | Hardening | Security/perf SLOs + real-account evidence bundles | hardening cont. |
| 12 | RC / buffer | Release packaging, sign-off, slack | — |

> The phase split (1+2+4+2+2) sums to 11; week 12 is buffer/RC. **Gaps resolution comes before hardening** — reach feature-complete first, then harden the stable system (hardening is the last engineering phase before RC).

## Open decisions (decide in week 1 — they affect G3/G5)
1. **Validation via CEL, not admission webhooks** — confirm CEL covers required-field, topology, and cluster-size checks.
2. **Single `Neo4j` CRD security model** — V1 security = admin secret + native auth + TLS only (no separate role/user/grant resources).
3. **TLS = user-supplied certificate secret** (cert-manager issuance is V2).

## Critical path
Week 1 design → G1 → G2 → **G3 Cluster/HA** (week 5) + **Azure adapter** (week 6) are the risk. Slippage eats the week 12 buffer first, then drops the hardening items (weeks 10–11) to V2 — never a core feature, since features are complete by the end of the gaps phase (week 9).
