# Knowledge Base

This directory is the operator's **structured, enforcement-tagged knowledge
base** for contributors — especially LLM-assisted ones (Claude Code, Cursor). It
exists so that the hard-won rules of this codebase live in **one home each**,
carry an explicit signal about *how strongly each rule is enforced*, and can be
validated against the real code instead of drifting into folklore.

## Why this exists

This knowledge base was created after `AGENTS.md` drifted into instructing agents
to rebuild a **banned** architecture (it told contributors to "preserve" a
centralized `{cluster}-backup` StatefulSet that had been deliberately removed,
and to wire admission webhooks the project forbids). Stale guidance that *reads*
authoritative is worse than no guidance: an LLM follows it confidently. The fix
is structural — documentation that is single-homed, enforcement-tagged, and
drift-checked against the code.

## The four legs

The contributor-readiness system has four parts; this directory is leg (b).

1. **The constitution — `AGENTS.md` (the front door).** A thin, *correct*
   document that every agent reads first. It states the project mission, the
   non-negotiable invariants (in summary), and *points here* for detail. It is
   the single source of truth for the invariants and must never contradict the
   code. It does **not** restate the full rule checklist.
2. **This knowledge base — `docs/knowledge/`.** The structured detail behind the
   constitution. See "How it's organized" below.
3. **Skills — `.claude/skills/`.** Invokable, step-by-step procedures distilled
   from real workflows (releasing, regenerating artifacts, debugging
   reconciliation, etc.). Skills *do*; the knowledge base *explains and
   constrains*.
4. **Enforced guardrails — CI guard scripts.** `scripts/check-invariants.sh`
   (added with this knowledge base) machine-checks the invariants in CI so they
   are not merely documented. `make check-drift` separately guarantees generated
   artifacts are never hand-edited out of sync.

## How it's organized

- **One concern per file.** Each file owns a single topic
  (e.g. `invariants.md`). A rule has exactly one home; it is never duplicated in
  another knowledge file, in `CLAUDE.md`, or in `AGENTS.md`. Other documents
  *link* to the home, they do not copy it. If you find the same rule in two
  places, that is a bug — collapse it to one home and link.
- **Structured entries.** Within a file, each rule is a structured entry with
  consistent fields. The flagship example, `invariants.md`, gives every
  invariant an `id`, a `rule`, a `WHY`, an explicit **enforcement status**, and a
  **violation symptom** (what an agent actually observes when they break it). The
  enforcement status uses a fixed vocabulary so the *strength* of each rule is
  unambiguous:
  - **CI-enforced (`scripts/check-invariants.sh`)** — machine-checked; the build
    fails on violation.
  - **test-pinned (`<test>`)** — a Go test asserts it; `make test-unit` fails.
  - **startup-checked** — the running operator refuses to start on violation.
  - **PROSE-ONLY — at risk** — convention/file-absence only; nothing actively
    rejects a violation. These are flagged honestly as the highest-risk rules.
- **`CLAUDE.md` is the constitution + index, not the encyclopedia.** The former
  79-rule checklist that lived in `CLAUDE.md` is being re-homed here. `CLAUDE.md`
  keeps the mission, the essential commands, and an *index that points into*
  `docs/knowledge/` — it does not also keep the full checklist. Single home, no
  duplication.
- **Per-package `CLAUDE.md` files load by locality.** Claude Code automatically
  loads the nearest `CLAUDE.md` for the files you're editing. Package-local
  guidance (e.g. a future `internal/controller/CLAUDE.md`) lives next to the code
  it governs so it surfaces exactly when relevant, instead of bloating the
  root-level document. The root `CLAUDE.md` carries only cross-cutting rules and
  the index into this knowledge base.

## Accuracy contract

Every code symbol cited in this knowledge base — file path, function name, test
name — **must exist in the current tree**. Before citing a symbol, verify it
(`grep`/read). When a rule's referenced symbol no longer exists, fix or remove
the rule rather than copying stale guidance forward. The drift between
`AGENTS.md` and the code is exactly the failure this contract prevents.

The guardrails enforce this contract two ways:

- **`scripts/check-invariants.sh`** (CI) machine-checks the invariants in
  `invariants.md` against the code — failing the build if a forbidden file,
  symbol, or pattern reappears, so the docs and the code cannot silently
  disagree.
- **`make check-drift`** (CI gate) regenerates every generated artifact
  (`sync-all` + `bundle`) and fails on any diff, ensuring nobody hand-edits a
  `# This file is GENERATED` file.

## For contributors starting out

Read in this order:

1. **`AGENTS.md`** — the front door: mission and invariants at a glance.
2. **`docs/knowledge/invariants.md`** — the five hard invariants in full, with
   their enforcement status and violation symptoms. Internalize these before
   writing code; they are the rules most expensive to break.
3. **The root `CLAUDE.md` index**, then the per-package `CLAUDE.md` nearest the
   code you're touching.
4. **`.claude/skills/`** — when you need to *perform* a procedure (release,
   regenerate artifacts, debug a reconcile).

When in doubt, trust the **code and the tests** over any prose, and trust
**enforcement-tagged prose** (CI-enforced / test-pinned) over **PROSE-ONLY**
guidance.
