# unlearn implementation plan

This checklist maps implementation work to the product design in `docs/superpowers/specs/2026-05-17-unlearn-skill-cleanup-workbench-design.md`.

## 1. Go CLI foundation

- [ ] Initialize Go module and dependency management.
- [ ] Add Cobra/Fang-style command tree with `unlearn`, `audit`, `scan`, and `restore`.
- [ ] Add Bubble Tea/Bubbles/Lip Gloss TUI foundation for the default dashboard command.
- [ ] Keep `mise` and pre-commit checks passing.

## 2. Persistent state/config

- [ ] Implement local state path abstraction for `index.db`, `quarantine/`, and `llm-cache/`.
- [ ] Implement TOML config/decision load/save.
- [ ] Store trusted roots, write permissions, LLM/history opt-ins, keep/ignore/drop decisions.
- [ ] Ensure no raw session excerpts are persisted by default.

## 3. Skill inventory engine

- [ ] Scan known global roots independently, not through `npx skills` output.
- [ ] Support user-provided roots and explicit trust flags for automation/tests.
- [ ] Record encountered path, resolved real path, symlink state, and broken symlinks.
- [ ] Support directory skills with `SKILL.md`.
- [ ] Support standalone markdown-file skills.
- [ ] Treat unknown skill-like shapes as read-only inventory items.
- [ ] Parse frontmatter and body.
- [ ] Extract explicit support-file references.
- [ ] Estimate token-cost lower/upper range.
- [ ] Infer display-only provenance.

## 4. Analysis engine

- [ ] Produce deterministic duplicate findings.
- [ ] Produce deterministic conflict findings.
- [ ] Produce deterministic overlap findings.
- [ ] Produce unseen findings from opt-in usage evidence.
- [ ] Produce high-token-cost and broad-activation-risk findings.
- [ ] Produce broken symlink/reference findings.
- [ ] Order cleanup candidates by severity and reasons without numeric scores.
- [ ] Define LLM-assisted analysis interface/stub with explicit limitations.
- [ ] Define history adapter interface and JSONL adapter for derived usage evidence.

## 5. Actions and safety

- [ ] Implement inspect data path.
- [ ] Implement keep and ignore-finding decisions.
- [ ] Implement quarantine with confirmation and write-permission gate.
- [ ] Implement restore from quarantine.
- [ ] Implement direct delete gates, including typed skill name for active skills.
- [ ] Implement rename dry-run and execution for directory + `SKILL.md` frontmatter.
- [ ] Warn/suggest quarantine for symlinked or package-managed rename targets.
- [ ] Implement batch dry-run summaries.
- [ ] Implement `audit --fix` safe fixes only.

## 6. TUI dashboard

- [ ] Default `unlearn` opens full-screen dashboard.
- [ ] Findings view is default.
- [ ] Skill inventory view is secondary.
- [ ] Compact density is default, rich density toggle exists.
- [ ] Render dynamic bottom key bar only.
- [ ] Support Vim keys and arrow keys.
- [ ] Detail pane explains selected skill/finding, token-cost range, activation risk, provenance, usage evidence, and available actions.

## 7. Quick commands

- [ ] `unlearn audit` prints concise read-only overview.
- [ ] `unlearn audit --fix` shows dry-run and confirmations for safe fixes.
- [ ] `unlearn scan` refreshes local index.
- [ ] `unlearn restore <skill>` restores quarantined skill.
- [ ] Avoid standalone `dedupe` or `resolve` commands.

## 8. Tests and fixtures

- [ ] Unit tests: frontmatter parsing.
- [ ] Unit tests: token estimation.
- [ ] Unit tests: reference extraction.
- [ ] Unit tests: symlink resolution.
- [ ] Unit tests: duplicate/conflict/overlap detection.
- [ ] Unit tests: activation risk.
- [ ] Unit tests: provenance inference.
- [ ] Unit tests: TOML decisions.
- [ ] Integration tests: fixture roots for duplicate/conflict/overlap.
- [ ] Integration tests: quarantine/restore.
- [ ] Integration tests: rename directory + frontmatter.
- [ ] Integration tests: trust/write gates.
- [ ] Integration tests: audit output.
- [ ] TUI model tests: view/density toggles, key handling, dynamic key bar/action availability.

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

M1: implement the smallest safe read-only audit slice, then commit before starting mutation work.
