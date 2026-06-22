---
name: add-regression-rule
description: Add a new invariant or regression rule to docs/knowledge/ in the project's structured, enforcement-tagged format — pinned to a REAL test/path, in the right file, optionally guarded by scripts/check-invariants.sh, and proven green by make check-knowledge-drift.
---

# Add a regression rule

`docs/knowledge/` is the **single home** for the operator's invariants and
regression rules. Each rule is a structured, enforcement-tagged entry that pins
itself to the code that enforces it — a `` `TestXxx` `` test name and/or a
backticked file path. This skill adds a new rule in that exact format and proves
every pin resolves, so the knowledge base stays grounded instead of drifting
into folklore (the failure that produced the whole knowledge base — see
`docs/knowledge/README.md`).

This is the *authoring* counterpart to the `fix-knowledge-drift` skill (which
*repairs* stale pins). Both obey the same accuracy contract: **every cited
symbol must exist in the current tree.**

## When to use

- You discovered a new hard constraint or regression and want it written down
  where the next agent will read it (and where `make check-knowledge-drift`
  keeps it honest) — not buried in a commit message.
- A bug got fixed and you're adding the rule that stops it from coming back, in
  the same PR as the fix and its pinning test.
- CLAUDE.md tells you "add it to the appropriate `docs/knowledge/` file (with an
  enforcement tag), not back into this file" — this is that procedure.

## Pick the right home (one rule, one file — never duplicate)

| Rule kind | File | Entry shape to copy |
|---|---|---|
| One of the 5 constitution-level hard constraints (webhooks, Kind-only, Enterprise images, V2_ONLY discovery, server-arch) | `docs/knowledge/invariants.md` | `id` / `rule` / `WHY` / `enforcement status` / `violation symptom` |
| Runtime/controller/validation rule (standalone, TLS, users/roles, auth, network/metrics) | `docs/knowledge/operations.md` | numbered `id` / `scope` / `rule` / `why` / `pinned-by` / `enforcement` |
| Backup / restore / sharding rule | `docs/knowledge/backup-restore.md` | `Scope` / `Rule` / `Why` / `Pinned-by` / `Status` |

If the same rule already lives in another file, CLAUDE.md, or `AGENTS.md`, that
is a duplication bug — link to the one home instead of copying (see
`docs/knowledge/README.md` "One concern per file").

**Enforcement-status vocabulary** (use the exact terms from `invariants.md`):
`guard-checked (scripts/check-invariants.sh)` (advisory grep), `test-pinned
(<test>)` (a Go test; blocking `unit-tests` job), `runtime-enforced` (a
validator in `internal/validation/` rejects the CR inline), `PROSE-ONLY — at
risk` (convention only — flag these honestly as highest-risk).

## Procedure

1. **Read the format you're about to match.** Open the target file and copy an
   existing entry's field shape verbatim — do not invent a new layout.
   ```bash
   sed -n '37,66p' docs/knowledge/invariants.md   # INV-1 entry shape
   sed -n '42,47p' docs/knowledge/operations.md   # operations id-4 entry shape
   ```

2. **Find the REAL pin before you write a word of prose.** A rule with no
   resolvable pin is worse than no rule. Grep the tree for the test that asserts
   the behavior and the file the rule governs:
   ```bash
   grep -rn "func TestYourBehavior" --include='*_test.go' .
   grep -rln "<a distinctive symbol from the code the rule governs>" --include='*.go' .
   ```
   Pin to a `` `TestName` `` that exists. If no dedicated unit test guards it,
   cite the integration test that covers it indirectly and **state the gap in
   plain words** (`no direct unit test — known gap`), exactly as
   `operations.md` id 14 / id 16 do. Never fabricate a pin to look enforced.

3. **Write the entry** in the chosen file, with every field filled and every
   file path / test name in backticks (the drift check extracts backticked
   `` `TestXxx` `` pins and backticked `internal/.../*.go`, `api/...`,
   `cmd/...`, `config/...yaml` paths and asserts each resolves).

4. **(Optional) Add a grep guard** for a *hard* invariant that a banned string
   would reintroduce. `scripts/check-invariants.sh` is portable bash 3.2 (no
   `grep -P`, no associative arrays). Copy an existing block's shape — the
   `grep_go_nontest '<pattern>'` helper scans non-test `*.go`; the `fail
   "<invariant-name>" "<msg>"` helper records a violation:
   ```bash
   sed -n '100,110p' scripts/check-invariants.sh   # INV-1b: grep_go_nontest + fail pattern
   ./scripts/check-invariants.sh                    # must still end: "OK — all 5 hard invariants hold."
   ```
   Keep `.claude/worktrees/` and `vendor/` excluded (the `EXCLUDE_RE` is already
   applied by the helpers). Only add a guard if a single grep pattern cleanly
   catches the violation with no false positives on the clean tree — otherwise
   leave enforcement to the test pin and say so.

5. **Prove every pin resolves — ALWAYS finish here.** The drift check is the
   mechanical half of the accuracy contract:
   ```bash
   make check-knowledge-drift
   # -> check-knowledge-drift: OK — N knowledge reference(s) all resolve.
   ```
   A `WARN [knowledge-drift]` line means a `` `TestXxx` `` pin isn't in any
   `*_test.go` or a backticked path doesn't exist — fix the pin (see the
   `fix-knowledge-drift` skill), do not delete the WARN.

6. **Run both guards together** (an entry plus a new grep touches both):
   ```bash
   make check-knowledge   # check-invariants + check-knowledge-drift
   ```

7. **Report**, as your final message: which file got the rule, its `id`, the
   exact test/path you pinned to (and any honest gap you flagged), and whether
   you added a `check-invariants.sh` guard.

## Guardrails

- This skill edits **only** `docs/knowledge/*.md` and, optionally,
  `scripts/check-invariants.sh`. It does not touch the code the rule governs —
  if the rule needs a new test to pin to, that test belongs in the same PR as
  the fix, authored separately.
- **Code wins over prose.** Pin to what the tree actually does today; never
  write a rule that describes how you wish the code worked.
- The guards are **advisory / non-blocking** in CI (the `Invariant Guards` job)
  and are run by the agent skills + `make check-invariants` /
  `make check-knowledge-drift`. The only **blocking** merge gates are
  `check-drift` (generated artifacts) and `unit-tests`. Do not claim a new grep
  guard "blocks merge" — a `test-pinned` rule under the `unit-tests` job is the
  thing that actually fails the build.
- Never re-add the rule to CLAUDE.md as inline prose — the 79-rule inline
  checklist was deliberately re-homed here; CLAUDE.md keeps only the invariants
  summary and an index.

## Why this exists / provenance

The knowledge base was created after `AGENTS.md` drifted into instructing agents
to rebuild a **banned** architecture, because rules lived in prose that read
authoritative but cited nothing checkable. The fix was structural: single-homed,
enforcement-tagged entries pinned to real tests/paths, with
`scripts/check-knowledge-drift.sh` mechanically asserting the pins resolve. This
skill is the disciplined add-path that keeps that contract intact — so a new
rule lands grounded from the start instead of becoming the next stale citation
the `fix-knowledge-drift` skill has to repair.
