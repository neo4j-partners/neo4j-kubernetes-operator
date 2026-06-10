# Neo4j Version Support Policy

This page defines which Neo4j versions the operator supports, and the policy by
which that set changes over time. The goal is a small, predictable support
matrix that never lags behind the database it manages.

## Supported versions (current)

| Track | Supported by the operator | Notes |
|---|---|---|
| **LTS** | **5.26.x** | Last SemVer release; the 5-line Long-Term-Support version. |
| **Feature (CalVer)** | **2025.x.x and later** (`2026.x`, …) | The rolling feature track. CalVer is detected automatically (`major >= 2025`), so future years work without a code change. |

Enterprise images only (`neo4j:5.26-enterprise`, `neo4j:2025.xx.x-enterprise`,
…). Community is not supported. Versions older than 5.26 (4.x, 5.0–5.25) are
**not** supported.

## Background: Neo4j's two release tracks

Since Neo4j 5, Neo4j ships on two tracks, and the cadence/lifecycle numbers are
what drive this policy:

| | LTS (feature-stable) | Feature release (CalVer) |
|---|---|---|
| Cadence | every ~2 years | frequent, cumulative |
| Vendor support lifecycle | ~3.5 years | only until the next release |
| Migration overlap | ~1 year between consecutive LTSs | n/a |
| Path to LTS | — | a feature line becomes the next LTS after ~2 years |

Because the **LTS support lifecycle (~3.5y) is longer than the LTS cadence
(~2y)**, two LTS lines are supported by Neo4j *at the same time* for roughly the
last ~1.5 years of the older one. That overlap is the crux of this policy.

Today this maps to: **5.26 = the active LTS**, **2025.x/2026.x = the feature
track**.

## The policy

> The operator supports **the current Neo4j LTS line plus the current CalVer
> feature line**. When a new LTS reaches GA it is **added**; the previous LTS is
> **dropped only when it reaches Neo4j end-of-life**, not when the new LTS ships.

Three rules make this precise:

1. **Add the new LTS at its GA.** The first operator release after a new LTS
   GAs adds it to the supported set and CI.
2. **Keep the old LTS until *Neo4j's* EOL for that line.** The operator must
   never refuse a Neo4j version that Neo4j itself still supports. Dropping the
   old LTS the moment a new one ships would strand still-supported production
   clusters on a frozen operator — so we hold it through the vendor overlap
   window, until the old LTS's ~3.5-year lifecycle ends.
3. **Track the feature line, not every point release.** "Supported CalVer"
   means the line, validated against the most recent CalVer at operator-release
   time. CalVer detection keeps the operator forward-compatible with future
   years; we do not pin or gate individual monthly releases.

### Steady state vs. transition

- **Steady state = exactly two anchors** (current LTS + current CalVer). This is
  the bounded matrix the policy optimises for — minimal CI cost and code paths.
- **Transition = briefly three anchors** (old LTS + new LTS + CalVer) during the
  vendor overlap window, narrowing back to two when the old LTS reaches Neo4j
  EOL. That short-lived third lane is the deliberate, time-boxed cost of not
  being stricter than the database.

### Worked example (5.26 → next LTS)

```
5.26 GA (Dec 2024) ──────────────────────────────── 5.26 Neo4j EOL (~mid 2028)
                              new CalVer LTS GA (~2yr later)
                              │
   operator supports:        │
   5.26 + CalVer ────────────┤ + new LTS  (3 anchors, overlap) ──┐
                             add new LTS                          drop 5.26 here
                                                                  (2 anchors again)
```

When the new LTS GAs, the operator supports **5.26 + new LTS + CalVer** for the
overlap. 5.26 is dropped only at *its* Neo4j EOL — not at the new LTS's launch.

## What "supported" means in the operator

- **Hard gate (rejected by the version validator):** genuinely unsupported
  versions — anything older than the current LTS (pre-5.26 today), or a line
  past its Neo4j EOL.
- **Soft (allowed, may warn):** a CalVer newer than the one the current operator
  release was validated against. A brand-new CalVer must not be rejected the day
  it ships, so the validator does not hard-block "newer than tested" within a
  supported track.
- **CI anchors:** the integration suites run against the supported anchors
  (today: `5.26-enterprise` + latest CalVer). The steady-state invariant is
  *exactly two* anchors; a transition window may run three.

## When the next LTS lands — maintenance checklist

The change is small and contained. Adding a new LTS / eventually dropping 5.26
touches:

- the Neo4j version validator's allowed/minimum set (`internal/validation/`),
- the CI matrix anchors (`.github/workflows/integration.yml`, `integration-tests.yml`),
- the "Supported Neo4j versions" line in `CLAUDE.md`,
- the support matrix at the top of this page.

## FAQ

**I'm on 5.26 and a new LTS just shipped — will the next operator drop me?**
No. 5.26 stays supported until Neo4j's own EOL for the 5.26 line (~mid 2028),
regardless of how many operator releases ship in between.

**Do you support every monthly CalVer release?**
We support the CalVer *line*, validated against the latest at each operator
release. Newer CalVers are accepted (forward-compatible detection); we just
can't claim to have explicitly tested a release that didn't exist yet.

**Why not just keep supporting every LTS forever?**
Each supported line multiplies CI cost, branch maintenance, and
inter-version compatibility surface. Bounding the matrix to "current LTS +
feature line" (briefly +1 during overlap) is the whole point — it keeps the
operator fast to maintain without ever lagging the database's own lifecycle.
