# Field analysis template

Copy to `design/analysis/helm-fields/fields/<helm-path>.md`.

```markdown
# `<helm_path>`

## Client need

[Who needs this and why — 2–4 sentences]

## Neo4j documentation

- [Title](URL) — section

## Helm implementation

- **Templates**:
- **Go model**:
- **K8s resources**:
- **Neo4j mechanism**:

## Category

topology | storage | network | config | security | scheduling | health | observability | plugins | lifecycle | packaging

## Semantic concerns

> From **helm-semantic-concern-mapper** — paths in the same concern may be far apart in values.yaml.

| concern_id | role in this concern | co-paths (scattered) |
|------------|----------------------|----------------------|
| CONCERN-TOPOLOGY | | neo4j.minimumClusterSize, analytics.*, … |
| | | |

## CRD mapping (draft)

- **Target**: `Neo4j.spec.<section>.<field>` | separate CRD | N/A
- **Notes**:

## Aggregation

- **Group**: AGG-* | none
- **Must decide with**:

## Versioning

- **Classification**: breaking | safe | deferred
- **Rationale**:

## FR / AC

- FR:
- AC:

## Open questions

-
```
