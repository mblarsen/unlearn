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
- [x] Implement direct delete gates, including typed skill name for active skills.
- [x] Implement rename dry-run and execution for directory + `SKILL.md` frontmatter.
- [x] Warn/suggest quarantine for symlinked or package-managed rename targets.
- [x] Implement batch dry-run summaries.
- [x] Implement `audit --fix` safe fixes only.

## 6. TUI dashboard

- [x] Default `unlearn` opens full-screen dashboard.
- [x] Findings view is default.
- [x] Skill inventory view is secondary.
- [x] Compact density is default, rich density toggle exists.
- [x] Render dynamic bottom key bar only.
- [x] Support Vim keys and arrow keys.
- [x] Detail pane explains selected skill/finding, token-cost range, activation risk, provenance, usage evidence, and available actions.

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

Initial v1 implementation is complete enough for fixture/temp-root validation. Remaining limitations to track after this pass: dashboard action keys show available actions but do not yet execute the full interactive mutation flows, LLM-assisted analysis is an opt-in stub, SQLite history adapters are not implemented, and first-launch setup is represented by explicit `--trust-root`, `--history-jsonl`, and `--with-llm` flags rather than a dedicated setup screen.
