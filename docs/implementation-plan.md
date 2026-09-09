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
- [x] Add generic SQLite history adapter with table discovery, text-column scanning, row limits, and derived evidence only.
- [x] Cache JSONL and SQLite history scan evidence through the same source-fingerprint cache.

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

## Architecture deepening — cancellable draft lifecycle

- [x] Inject the context-aware merged-draft generator instead of constructing it inside each request.
- [x] Centralize draft operation start, cancellation, identity, and result acceptance behind one lifecycle implementation.
- [x] Reject stale results when operation A completes after cancellation and operation B has started.
- [x] Preserve content-hash caching, opt-in privacy, and read-only draft preview behavior.
- [x] Cover controlled A-start/cancel/B-start/A-finish/B-finish ordering and run the full validation suite.

## Current focus

### Architecture refactor — deep audit orchestration

- [x] Add one `internal/audit` interface that accepts scan policy/context and returns inventory, findings, typed evidence coverage, skipped roots, and diagnostics.
- [x] Derive unseen-finding eligibility from coverage inside the audit module; callers must not use nullable evidence maps as policy.
- [x] Preserve trusted-root selection, explicit history opt-in/privacy, derived-evidence caching, progress, cancellation, LLM fallback, and generated summaries.
- [x] Move dashboard snapshot cache preference/refresh behavior behind the audit interface without adding filesystem or SQLite ports.
- [x] Inject only the true-external LLM analyzer; test the interface with temporary roots, real SQLite, and a controlled analyzer.
- [x] Replace CLI orchestration tests with audit-interface regression coverage, then keep CLI behavior checks focused on output and persisted opt-ins.
- [x] Run `go test ./...`, `go build ./...`, and `mise run check` before completion.

Initial v1 implementation is complete enough for fixture/temp-root validation and interactive QA. Remaining limitations to track after this pass: LLM-assisted analysis now has minimal Gemini REST support for cached summaries, semantic-overlap groups, and preview-only merged-skill draft generation behind `--with-llm`/setup opt-in plus `GEMINI_API_KEY`/`GOOGLE_API_KEY`, but richer provider selection remains future work; Pi history discovery is bounded to known JSONL session locations and stores paths/derived evidence only, SQLite history scanning covers explicit database paths plus bounded discovery under configured scan roots, and batch cleanup is specialized for duplicate installs by root rather than arbitrary multi-select across all finding types.

## Priority 4 — preview-only merged skill drafts

- [x] Add an LLM draft-generation interface and Gemini implementation for selected skill metadata/content.
- [x] Cache draft results by selected content hashes plus provider/model/prompt version.
- [x] Add dashboard merge-draft flow with arbitrary logical skill selection.
- [x] Group and preselect the current overlap finding's skills when invoked from an overlap detail.
- [x] Keep the flow preview-only: display a read-only `SKILL.md` draft and do not mutate skill files.
- [x] Show friendly advisory status when LLM-assisted draft generation is disabled or missing API credentials.
- [x] Cover picker selection, arbitrary skills, overlap grouping/preselection, generator success/failure, cache behavior, Gemini prompting, and no skill inventory mutation with tests.

## Issue #2 — Pi history scan flow

- [x] Keep Pi JSONL discovery bounded and read-only until explicit opt-in.
- [x] Add cancellation-aware JSONL scanning with progress callbacks.
- [x] Print per-file history scan progress from `unlearn scan` without persisting raw excerpts.
- [x] Carry derived history evidence and source counts onto inventory skills.
- [x] Surface derived history evidence in dashboard finding and skill details.
- [x] Cover progress, cancellation, CLI scan output, and dashboard history surfacing with tests.

## QA fix — missing persisted history sources

- [x] Skip configured or discovered history sources that disappear before scanning.
- [x] Keep explicit missing paths and non-missing source errors fatal.
- [x] Preserve cached positive evidence on ordinary loads, ignore it on forced rescans, and suppress unseen findings when source coverage is incomplete.
- [x] Report skipped history paths without reading or storing raw session content.

## Issue #1 — harness-aware roots

- [x] Use `vercel-labs/skills/src/agents.ts` as the source reference for supported agent skill roots.
- [x] Add a Go-side agent catalog with display name, project root, global root, detection paths, and env/XDG overrides.
- [x] Persist active and inactive harness selections in TOML config.
- [x] Extend first-launch setup with active/inactive/off harness toggles.
- [x] Derive default scan roots from selected harnesses instead of a four-root hardcoded list.
- [x] Attach active/inactive harness ownership metadata to scanned skills.
- [x] Only report duplicate/conflict findings when installs are visible to at least one shared active harness.
- [x] Report skills installed only in inactive harness roots as cleanup candidates.
- [x] Cover agent catalog, config, setup, duplicate semantics, and inactive-root findings with tests.

## QA fix — stale destructive-action inventory

- [x] Reconcile externally deleted install paths when loading the dashboard cache and persist the pruned inventory.
- [x] Treat already-missing delete/quarantine targets as stale inventory, not destructive-action failures.
- [x] Track completed targets incrementally so partial batch failures update the model and SQLite index without hiding the error.
- [x] Keep write checks ahead of batch mutation and continue deleting symlink entries without following their targets.
- [x] Confirm a quarantine source is absent before classifying an `ENOENT` as stale.
- [x] Treat an empty dashboard cache as a rescan signal so later external installs remain discoverable.
- [x] Cover all-missing, mixed, normal multi-delete, partial failure, symlink safety, model feedback, and persisted restart behavior with temporary fixtures.

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

## QA fix — reachable TUI viewports

- [x] Replace silent width inflation with an explicit 80×18 minimum-size gate.
- [x] Keep install, restore, and batch picker cursors visible for long lists.
- [x] Reserve modal option rows when confirmation content exceeds the viewport.
- [x] Wrap selected install paths and provenance, with Page Up/Page Down access when a selected path exceeds the picker viewport.
- [x] Cover 80×24, 120×40, 200×60, narrow, short, long-list, and long-path renders with fixture-only tests.

## Architecture refactor — deep picker module

- [x] Move cursor navigation, marked selection, selected-row scrolling, resize, and bounded rendering behind one picker interface.
- [x] Use the picker module for install, restore, and batch-root flows while keeping install selection precedence in `actions.ResolveSelection`.
- [x] Preserve the fixed modal option tail and make every part of a long selected path reachable with PgUp/PgDn.
- [x] Test picker behavior through its interface and retain dashboard viewport regression coverage.

Relevant commits: `0d8c029`, `b291529`, `99099c6`, `b9b9eff`, `0054000`, `f62bb1a`, `424c699`, `8058fc6`, `fd055a6`, `7ac6b68`.

## QA fix — persistent feedback and action discovery

- [x] Show complete wrapped status and error messages with error recovery and explicit dismissal.
- [x] Reserve help and quit in the normal footer, and put cleanup actions before merge drafting.
- [x] Add contextual help for findings and skill inventory views.
- [x] Use consequence-specific confirmation titles and labels for destructive actions.
- [x] Explain empty findings and inventory states with the next available action.
- [x] Cover feedback at 80×24, 120×40, and 200×60 with deterministic model render tests.
