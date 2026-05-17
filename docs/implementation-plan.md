# unlearn implementation plan

This checklist maps implementation work to the product design in `docs/superpowers/specs/2026-05-17-unlearn-skill-cleanup-workbench-design.md`.

## 1. Go CLI foundation

- [x] Initialize Go module and dependency management.
- [x] Add Cobra/Fang-style command tree with `unlearn`, `audit`, `scan`, and `restore`.
- [x] Add Bubble Tea/Bubbles/Lip Gloss TUI foundation for the default dashboard command.
- [x] Keep `mise` and pre-commit checks passing.

## 2. Persistent state/config

- [x] Implement local state path abstraction for `index.db`, `quarantine/`, and `llm-cache/`.
- [x] Implement TOML config/decision load/save.
- [x] Store trusted roots, write permissions, LLM/history opt-ins, keep/ignore/drop decisions.
- [x] Ensure no raw session excerpts are persisted by default.

## 3. Skill inventory engine

- [x] Scan known global roots independently, not through `npx skills` output.
- [x] Support user-provided roots and explicit trust flags for automation/tests.
- [x] Record encountered path, resolved real path, symlink state, and broken symlinks.
- [x] Support directory skills with `SKILL.md`.
- [x] Support standalone markdown-file skills.
- [x] Treat unknown skill-like shapes as read-only inventory items.
- [x] Ignore scanner/root metadata entries such as `.system`.
- [x] Parse frontmatter and body.
- [x] Extract explicit support-file references.
- [x] Estimate token-cost lower/upper range.
- [x] Infer display-only provenance.

## 4. Analysis engine

- [x] Produce deterministic duplicate findings.
- [x] Produce deterministic conflict findings.
- [x] Produce deterministic overlap findings.
- [x] Produce unseen findings from opt-in usage evidence.
- [x] Produce high-token-cost and broad-activation-risk findings.
- [x] Produce broken symlink/reference findings.
- [x] Order cleanup candidates by severity and reasons without numeric scores.
- [x] Define LLM-assisted analysis interface/stub with explicit limitations.
- [x] Define history adapter interface and JSONL adapter for derived usage evidence.

## 5. Actions and safety

- [x] Implement inspect data path.
- [x] Implement keep and ignore-finding decisions.
- [x] Implement quarantine with confirmation and write-permission gate.
- [x] Implement restore from quarantine.
- [x] Implement direct delete gates with exact install selection and modal confirmation.
- [x] Implement duplicate install action selection, space-based multi-select, and `All N installs` for quarantine/delete.
- [x] Implement restore through a navigable quarantined-skill modal list.
- [x] Implement batch duplicate cleanup by root.
- [x] Implement rename dry-run and execution for directory + `SKILL.md` frontmatter.
- [x] Warn/suggest quarantine for symlinked or package-managed rename targets.
- [x] Implement batch dry-run summaries.
- [x] Implement `audit --fix` safe fixes only.

## 6. TUI dashboard

- [x] Default `unlearn` opens full-screen dashboard.
- [x] Findings view is default.
- [x] Skill inventory view is secondary.
- [x] Compact density is default, rich density toggle exists.
- [x] Rich mode focuses selected finding/install detail; skill inventory rows stay compact.
- [x] Render dynamic bottom key bar only with width-aware truncation.
- [x] Support Vim keys and arrow keys.
- [x] Support control-chord action shortcuts (`ctrl+q`, `ctrl+d`, `ctrl+r`, `ctrl+u`, `ctrl+k`, `ctrl+g`, `ctrl+b`).
- [x] Detail pane explains selected skill/finding, token-cost range, activation risk, provenance, usage evidence, and available actions.
- [x] Duplicate/conflict details are comparison-first with `tab` / `shift+tab` focused-install cycling.
- [x] Action confirmations and selections use modal overlays.

## 7. Quick commands

- [x] `unlearn audit` prints concise read-only overview.
- [x] `unlearn audit --fix` shows dry-run and confirmations for safe fixes.
- [x] `unlearn scan` refreshes local index.
- [x] `unlearn restore <skill>` restores quarantined skill.
- [x] Avoid standalone `dedupe` or `resolve` commands.

## 8. Tests and fixtures

- [x] Unit tests: frontmatter parsing.
- [x] Unit tests: token estimation.
- [x] Unit tests: reference extraction.
- [x] Unit tests: symlink resolution.
- [x] Unit tests: duplicate/conflict/overlap detection.
- [x] Unit tests: activation risk.
- [x] Unit tests: provenance inference.
- [x] Unit tests: TOML decisions.
- [x] Integration tests: fixture roots for duplicate/conflict/overlap.
- [x] Integration tests: quarantine/restore.
- [x] Integration tests: rename directory + frontmatter.
- [x] Integration tests: trust/write gates.
- [x] Integration tests: audit output.
- [x] TUI model tests: view/density toggles, key handling, dynamic key bar/action availability.
- [x] Setup model tests: trust toggles, LLM/history opt-ins, TOML persistence shape, bounded Pi JSONL discovery.
- [x] TUI action tests: keep, ignore finding, quarantine, delete, rename, restore, write gates, confirmations, and warning states through injected action service.
- [x] TUI action tests: exact install selection, duplicate multi-select, `All N installs`, restore modal list, focused-install cycling, and batch duplicate cleanup.

## Milestones

### M1: Safe read-only audit slice

- CLI foundation.
- Config/state paths.
- Trusted fixture/global-root scanning.
- Skill parsing, reference extraction, token estimates, provenance basics.
- Deterministic duplicate/conflict/broken/high-token/broad-risk findings.
- Read-only `scan` and `audit`.
- Initial TUI model skeleton.
- Unit/integration tests for the read-only path.

### M2: Safety-gated mutations

- Write permission gate.
- Quarantine/restore.
- Rename dry-run/execution.
- Delete gates.
- `audit --fix` dry-run and exact-duplicate quarantine.
- Mutation-focused tests.

### M3: Workbench UX and optional intelligence

- Full list/detail dashboard polish.
- Usage evidence JSONL adapter.
- LLM-assisted interface/stub and cache plumbing.
- Rich detail fields and action availability.
- TUI model coverage.

## Current focus

Initial v1 implementation is complete enough for fixture/temp-root validation and interactive QA. Remaining limitations to track after this pass: LLM-assisted analysis is an opt-in stub, SQLite history adapters are not implemented, Pi history discovery is bounded to known JSONL session locations and stores paths/derived evidence only, and batch cleanup is specialized for duplicate installs by root rather than arbitrary multi-select across all finding types.

## QA notes — 2026-05-17 UI/UX cleanup

Manual deterministic render QA used a temporary in-repo harness with fixture-only roots under `/tmp/unlearn-qa`; no real installed skills or agent configs were scanned or modified. Fixture shape: 11 logical skills (`macos-calendar`, `macos-notes`, `macos-reminders`, `fastmail`, `mcp2cli`, `wrangler`, `ui-ux-pro-max`, `frontend-design`, `work-on-ticket`, `improve-codebase-architecture`, `self-improving-agent`) with two installs each, high token ranges, high activation risk, and generic broad descriptions. Render checks covered setup at ~90×25, findings dashboard at ~90×25, grouped skill inventory at ~90×25, and wider inventory at ~120×35.

Observations from this pass:

- Generic prose/action words no longer create one giant overlap cluster; the broad fixture produced duplicate, high-token, and broad-activation findings without overlap spam.
- The setup screen keeps status labels intact at 90 columns (`not trusted`, `missing`) and truncates paths/descriptions deliberately.
- The dashboard now uses a compact header, grouped finding sections, selected-row highlight, badges, summarized details, and a width-aware keybar that preserves core keys and shows `…` when lower-priority actions do not fit.
- The skill inventory consolidates repeated installs into one logical row with instance count and summarized roots instead of repeating same-skill rows.

## QA notes — 2026-05-17 interactive action refinements

Interactive QA extended the original dashboard interaction plan. The implemented behavior now differs from the original draft in these accepted ways:

- Action confirmations and selection flows are centered modal overlays rather than detail-pane-only interaction states.
- Destructive/action shortcuts use control chords: `ctrl+q` quarantine, `ctrl+d` delete, `ctrl+r` rename, `ctrl+u` restore/undo, `ctrl+k` keep, `ctrl+g` ignore finding, and `ctrl+b` batch duplicate cleanup. `ctrl+g` is used because `ctrl+i` is indistinguishable from Tab in terminals.
- Duplicate install actions require choosing exact install(s), support space-based multi-select, and include an explicit `All N installs` option for quarantine/delete.
- Duplicate cleanup can be batched by root with `ctrl+b`, quarantining duplicate installs from a selected root across many duplicate findings.
- Restore uses a navigable modal list of quarantined skills instead of a typed skill-name prompt.
- Duplicate/conflict details are comparison-first: `tab` and `shift+tab` cycle the focused install, and actions default to that focused install unless the user selects multiple installs or `All N installs`.
- Rich mode now expands focused finding/install hints; skill inventory rows stay compact in both density modes.
- Finding section headers use a subtle accent marker to strengthen hierarchy.
- Scanner ignores `.system` entries under skill roots so agent metadata does not appear as a fake skill.
- Equal token bounds render as a single compact value, such as `2.6k`, rather than a repeated range like `2.6k–2.6k`.

Relevant commits: `0d8c029`, `b291529`, `99099c6`, `b9b9eff`, `0054000`, `f62bb1a`, `424c699`, `8058fc6`, `fd055a6`, `7ac6b68`.
