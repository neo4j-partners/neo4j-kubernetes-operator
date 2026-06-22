---
name: fix-knowledge-drift
description: Detect and repair stale references in docs/knowledge/ — when a rule cites a test name or file path that no longer resolves, find what it became and re-pin it (or honestly flag the gap), then prove the drift check is green again.
---

# Fix knowledge-base drift

`docs/knowledge/` is the single home for the project's invariants and regression
rules. Each rule pins itself to the code that enforces it — a `` `TestXxx` ``
test name or a backticked `` `internal/.../foo.go` `` file path. Code gets
renamed and moved; the docs keep citing the old symbol. That rot is exactly how
a previous `AGENTS.md` ended up describing a **banned** architecture as if it
were current. This skill repairs that drift so the knowledge base stays grounded
in the real tree.

## When to use

- `make check-knowledge-drift` (or the advisory `Invariant Guards` CI job) reports
  one or more `WARN [knowledge-drift]` lines.
- You just renamed/moved a test or source file that a knowledge rule cites.
- You added or edited a rule in `docs/knowledge/` and want to confirm every
  reference resolves before committing.

## What the check verifies (so you know what can break)

`scripts/check-knowledge-drift.sh` extracts two high-signal reference shapes from
`docs/knowledge/*.md` and asserts each resolves:

1. **Test-name pins** — backticked `` `TestXxx` `` and `` `_FooBar` `` continuation
   fragments (the "Pinned by `TestA` + `_B`" pattern) must appear in some
   `*_test.go`.
2. **File-path refs** — backticked paths like
   `` `internal/controller/x.go` ``, `` `api/v1beta1/y.go` ``, `` `cmd/main.go` ``,
   `` `config/...yaml` `` must exist on disk.

It deliberately does **not** validate every backticked token (config keys,
Cypher, env vars produce too much noise). So a green check means the *pins*
resolve — not that the prose is correct.

## Procedure

1. **See the drift.** Run the check and read every WARN — each names the exact
   unresolved reference:
   ```bash
   make check-knowledge-drift
   ```
   Each line is either `test pin \`Sym\` not found in any *_test.go` or
   `referenced path \`p\` does not exist`.

2. **For each unresolved reference, find ground truth — do NOT guess.** The whole
   point of the knowledge base is to be true, so resolve every WARN by inspecting
   the real tree:
   - **Renamed/moved test:** find where the assertion lives now.
     ```bash
     grep -rn "func TestNewName" --include='*_test.go' .
     # or search by the behavior the rule describes:
     grep -rln "SHOW SERVERS\|<the thing the rule is about>" --include='*_test.go' .
     ```
     Re-pin the rule to the current `` `TestName` `` (and update the prose if the
     test's shape changed).
   - **Renamed/moved file:** find the new path and update the backticked ref.
     ```bash
     git log --oneline --follow -- <old/path> | head     # trace the rename
     grep -rln "<a distinctive symbol from that file>" --include='*.go' .
     ```
   - **Genuinely removed:** the code the rule pinned no longer exists. Decide
     honestly which case it is:
       - the *rule* is obsolete → remove or rewrite the rule, and drop its
         `MEMORY.md`/index pointer if any;
       - the rule is still true but its enforcement disappeared → say so in the
         rule's enforcement line (downgrade to `PROSE-ONLY — at risk` or point to
         the real, weaker signal) rather than inventing a citation.

3. **Never invent a citation to silence the check.** If no direct test exists,
   cite the test that *does* cover it (even indirectly) and state the gap in
   plain words. Precedent: a rule once cited `internal/neo4j/users_test.go`,
   which never existed — it was repaired by pointing to the real
   `test/integration/neo4juser_test.go` and adding "no direct unit test — known
   gap; enforcement is integration test (indirect) + code review." An honest
   weaker pin beats a fabricated strong one.

4. **Re-run until clean.**
   ```bash
   make check-knowledge-drift
   # -> check-knowledge-drift: OK — N knowledge reference(s) all resolve.
   ```

5. **Sanity-check the sibling guard too** (a rename often touches both):
   ```bash
   make check-knowledge      # runs check-invariants + check-knowledge-drift
   ```

6. **Report**, as your final message: each WARN, what it pointed at, what it
   became, and the fix you made (re-pin / rewrite / honest-gap note). Call out
   any rule you downgraded or removed so a human can sanity-check the call.

## Guardrails

- This skill edits **only** `docs/knowledge/` (and its `MEMORY.md`/index pointers).
  Do not "fix" drift by renaming code back to match a stale doc — when doc and
  code disagree, **the code wins**; the doc is what moves.
- The check is **advisory / non-blocking** in CI by design. Green here is a
  quality signal, not a merge gate — but a stale knowledge base actively misleads
  the next agent, so treat a WARN as worth fixing, not ignoring.

## Why this exists / provenance

Built after a fan-out authoring pass invented a plausible-but-nonexistent test
path (`internal/neo4j/users_test.go`) in `docs/knowledge/operations.md`. The
drift check caught it immediately; the fix was to cite the real integration test
and flag the missing unit test honestly. That episode is the reason both the
drift check and this repair procedure exist: LLM-authored docs hallucinate
citations, and the only durable defense is a mechanical check plus a disciplined
repair loop that always re-grounds in the real tree.
