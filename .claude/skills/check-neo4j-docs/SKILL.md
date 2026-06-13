---
name: check-neo4j-docs
description: Ground Neo4j behavior in the version-correct official manual before coding or verifying. When touching config settings, Cypher, procedures, topology, property sharding, or end-to-end processes, read the operations manual for the TARGET version (CalVer → /current/, LTS 5.26 → /5/) — never trust training memory, which drifts to deprecated 4.x patterns.
---

# Check Neo4j docs (version-correct grounding)

An LLM's training data is saturated with Neo4j **4.x**-era configuration, Cypher,
and procedures. This operator supports **only** Neo4j **5.26.x LTS** and
**CalVer (2025.x / 2026.x)** — there is no 4.x, and no 5.27–5.x semver (Neo4j
switched to CalVer after 5.26). So memory is the wrong source of truth: it
silently reintroduces removed 4.x constructs that compile fine, pass unit tests,
and then fail against a real 5.26+/CalVer database. The fix is mechanical: read
the official manual for the version you are targeting, and cite it.

This is the external counterpart to "grep before you cite": **read the manual
before you assert how Neo4j behaves.**

## When to use

Any time you write or verify something Neo4j-specific:
- a config setting (anything that lands in `neo4j.conf`, an env var, or
  `spec.config`);
- Cypher — especially DDL (`CREATE/ALTER DATABASE`, `SHOW …`), and
  privilege/role/user statements;
- a procedure call (`apoc.*`, `gds.*`, `db.*`, `dbms.*`,
  `fleetManagement.*`);
- topology / clustering / discovery behavior;
- property sharding;
- an end-to-end process (backup/restore, seeding, upgrades).

## Which manual (pick by the TARGET version)

Determine the target version from the cluster/standalone image tag (the operator
detects this via `neo4j.ParseVersion` → `IsCalver` when `major >= 2025`):

| Target | Manual |
|---|---|
| **CalVer** 2025.x / 2026.x (`-enterprise`) | https://neo4j.com/docs/operations-manual/current/ |
| **LTS** 5.26.x (`-enterprise`) | https://neo4j.com/docs/operations-manual/5/ |

Cypher syntax has its own manual (Cypher 5 for 5.26; Cypher 25 for 2025.x) — link
through from the operations manual's Cypher section. If a feature differs between
the two supported lines (e.g. discovery endpoint keys, `DEFAULT LANGUAGE` in
CREATE DATABASE), check **both** and gate the operator's output on the version.

## Procedure

1. **Identify the target version** from the image tag in the CR / test /
   sample you are working against. CalVer → `/current/`, 5.26.x → `/5/`.

2. **Fetch the relevant manual page** (use WebFetch) and search it for the exact
   setting / procedure / statement. Don't skim from memory — confirm it on the
   page:
   ```
   WebFetch(url="https://neo4j.com/docs/operations-manual/current/<section>",
            prompt="Does setting X / procedure Y exist in this version? Is it
                    deprecated or removed? What is the current name and syntax?")
   ```
   For LTS, swap `/current/` → `/5/`.

3. **Confirm current, not deprecated/removed.** If the page marks it deprecated
   or you can't find it, it is almost certainly a 4.x ghost — find the modern
   replacement on the page and use that.

4. **Cite the manual** in the code comment or PR description (section + version),
   so the next reader/agent can re-check.

5. **Cross-check the operator's known mapping** in `CLAUDE.md` — it already lists
   the high-frequency offenders. Quick reference of 4.x → modern (always verify
   against the manual, this is a memory aid not the source of truth):

   | Banned 4.x (memory drift) | Modern 5.26+/CalVer |
   |---|---|
   | `dbms.mode=SINGLE` | (removed — server self-organizes) |
   | `causal_clustering.*` | `dbms.cluster.*` / discovery v2 |
   | `dbms.connector.*` | `server.*` |
   | `CALL dbms.cluster.role()` | `SHOW SERVERS` / `SHOW DATABASES` |
   | `CREATE DATABASE … OPTIONS {primaries:…}` | `… TOPOLOGY n PRIMARIES m SECONDARIES` |
   | `server.groups`, `metrics.bolt.*`, `dbms.cluster.role` | removed |
   | V1 discovery (port 5000) | **V2_ONLY**, port 6000 (invariant 4) |

## Guardrails

- This repo supports ONLY 5.26.x LTS + CalVer. If the manual shows a feature
  only in 4.x, it does not belong in the operator — full stop.
- CalVer is **validated-vs-best-effort**: each release pins one anchor CalVer in
  CI and stands behind it; a newer CalVer is allowed but may break (the operator
  emits strictly-validated config/Cypher). When in doubt, check the anchor
  version's manual.
- Don't paste long doc excerpts into code; cite the section + version and emit
  the version-correct config/Cypher.

## Why this exists / provenance

Reading the version-correct operations manual repeatedly caught the LLM baking
Neo4j-4.x-era settings, Cypher, and procedures into an operator that only
supports 5.26.x LTS and CalVer — drift that unit tests don't catch because the
4.x string compiles and only fails against a live modern database. Consulting
`/current/` (CalVer) or `/5/` (LTS) at authoring time, not after, is the cheapest
correction for an entire class of version-drift bugs.
