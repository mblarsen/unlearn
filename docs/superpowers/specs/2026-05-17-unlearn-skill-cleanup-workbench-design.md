# unlearn Skill Cleanup Workbench Design

Date: 2026-05-17
Status: Draft for user review

## Summary

`unlearn` is a dashboard-first Go CLI for cleaning up global AI-agent skills. It audits skills across known global roots, explains what each skill does, detects duplicates/conflicts/overlaps, estimates token cost, optionally scans history for actual invocation evidence, and lets the user selectively keep, ignore, quarantine, delete, or rename skills.

The product direction is a full-screen terminal cleanup workbench built with Charmbracelet tooling, complemented by a small set of useful quick commands.

## Goals

- Audit global skill installations across common agent roots.
- Help the user understand what each skill does from frontmatter/body content.
- Estimate token-cost range from `SKILL.md` and explicitly referenced support files.
- Surface aggressive cleanup findings without taking destructive action automatically.
- Support dedupe, conflict resolution, unused-skill review, and selective removal.
- Be symlink-aware and provenance-aware.
- Keep LLM analysis and history scanning opt-in.
- Use conservative filesystem safety: trusted roots, write permissions, dry-run previews, and confirmations.

## Non-goals for v1

- Editing agent configuration files.
- Treating `npx skills` output as the source of truth.
- Repointing copied duplicates to symlinks.
- Automated skill merging.
- Agent-specific variant registration.
- Root-first dashboard view.
- Numeric cleanup scores.

## Architecture

`unlearn` has three layers.

### 1. Inventory engine

The inventory engine discovers and parses skills.

Responsibilities:

- Scan known global skill roots independently:
  - `~/.agents/skills`
  - `~/.pi/agent/skills`
  - `~/.codex/skills`
  - `~/.config/opencode/skills`
- Support user-provided roots.
- Require first-time trust per skill root before scanning.
- Resolve symlinks and record both encountered path and resolved real path.
- Compute content hashes.
- Parse `SKILL.md` frontmatter and body.
- Support standalone markdown-file skills as secondary format.
- Treat unknown skill-like shapes as read-only inventory items.
- Extract explicit support-file references from `SKILL.md`.
- Estimate token-cost ranges.
- Infer display-only provenance from paths, symlink targets, package roots, git remotes, and install layout.

### 2. Analysis engine

The analysis engine produces findings from the inventory.

Findings:

- `duplicate`: same name and identical effective content.
- `conflict`: same name and different effective content.
- `overlap`: different names but similar purpose, trigger area, or operational role.
- `unseen`: no actual invocation evidence from opted-in history scans.
- `high token cost`: large context footprint.
- `broad activation risk`: likely to trigger for many requests.
- `broken symlink/reference`: missing target or referenced support file.

Deterministic analysis is the baseline. Optional LLM-assisted analysis can generate clearer summaries and improve semantic overlap grouping after first-launch opt-in or `--with-llm`.

### 3. UX/actions layer

The UX/actions layer owns the TUI, quick commands, and filesystem operations.

Responsibilities:

- Open the full-screen dashboard with `unlearn`.
- Provide quick commands:
  - `unlearn audit`
  - `unlearn audit --fix`
  - `unlearn scan`
  - `unlearn restore <skill>`
- Enforce trusted root scanning.
- Enforce separate write permission before modifying skills in a trusted root.
- Show dry-run previews for batch operations.
- Confirm destructive actions.
- Never edit agent configuration files in v1.

## Dashboard UX

`unlearn` opens into a finding-first terminal dashboard.

### View modes

The dashboard has two view modes:

- **Findings view**: default. Groups cleanup work by issue type.
- **Skill inventory view**: secondary. Lists skills directly by name for browsing/searching.

There is no root-first view in v1.

### Density modes

The dashboard has two density modes:

- **Compact**: default. Optimized for scanning many findings quickly.
- **Rich**: expands rows or detail panes with summaries, token-cost range, activation risk, provenance, usage evidence, and actions.

### Layout

The dashboard uses a list/detail workbench with a dynamic key bar at the bottom.

```text
┌ Findings / Skills ──────────┬ Details ─────────────────────────────┐
│ Duplicates (3)               │ selected skill/finding               │
│   caveman-help               │ what it does / why flagged           │
│ Conflicts (1)                │ token cost / usage / provenance      │
│   caveman                    │ available actions                    │
│ Overlaps (8)                 │                                      │
├──────────────────────────────┴──────────────────────────────────────┤
│ j/k/↑/↓ move · s skill inventory · d details · q quit               │
└─────────────────────────────────────────────────────────────────────┘
```

Navigation supports common Vim keys and arrow keys. Available actions and shortcuts are shown only in the dynamic bottom key bar, not duplicated as fixed labels in the main content.

### Actions

Dashboard action vocabulary is direct:

- `inspect`
- `keep`
- `ignore finding`
- `quarantine`
- `delete`
- `rename`

No branded synonyms such as `forget`, `stash`, or `protect` in v1.

## First-launch and permissions

First launch uses one setup screen, not a multi-step wizard.

The setup screen shows:

- discovered candidate skill roots
- trust toggles per root
- LLM-assisted analysis toggle
- local history scanning toggle

Example:

```text
unlearn setup

Skill roots:
  [x] ~/.agents/skills          scan trusted
  [ ] ~/.pi/agent/skills        not yet trusted
  [ ] ~/.codex/skills           not yet trusted
  [ ] ~/.config/opencode/skills not yet trusted

Options:
  [ ] Enable LLM-assisted summaries and overlap detection
  [ ] Scan local agent histories for actual invocation evidence

Continue → build inventory
```

Safety stack:

```text
root scan trust → optional history/LLM opt-ins → write permission → action confirmation
```

Trust decisions are stored in TOML. Automation can use an explicit flag such as `--trust-root <path>`.

## Data and state

`unlearn` keeps persistent local state to avoid expensive recomputation and preserve user decisions.

Suggested location:

```text
~/.local/state/unlearn/
  index.db
  quarantine/
  llm-cache/
```

Human-editable config/decisions should use TOML, for example:

```toml
llm_assisted = false
history_scan = false

[keep]
skills = ["ask-user", "brainstorming"]

[ignore_findings]
"overlap:wrangler:cloudflare" = "Known overlap"

[drop_candidates]
skills = ["caveman-help"]
```

### SQLite index

Use `modernc.org/sqlite` for the local index to avoid CGO.

Store:

- discovered skill instances
- encountered paths and resolved real paths
- symlink status
- content hashes
- parsed frontmatter
- token-cost estimates
- findings
- derived usage evidence
- cached scan timestamps

### Decisions

V1 decisions are keyed primarily by skill name for simplicity. The UI should show enough context to let users revise decisions if skill content changes.

### Quarantine

Quarantine moves active skill files/directories into state:

```text
~/.local/state/unlearn/quarantine/<timestamp>/<skill-name>/
```

`unlearn restore <skill>` restores from quarantine.

No raw session excerpts are stored by default. LLM-generated summaries are cached by skill content hash and labeled with provider/model.

## Token cost and activation risk

Token cost is a range:

- lower bound: primary `SKILL.md`
- upper bound: `SKILL.md` plus explicitly referenced support material

Referenced support material includes files linked or mentioned from `SKILL.md`. `unlearn` does not blindly count every file in the skill directory. Unreferenced markdown may be surfaced separately as a warning.

Activation risk estimates how likely a skill is to be pulled into agent context, based on breadth of frontmatter description, trigger language, and overlap with common user requests.

## Usage evidence

History scanning is opt-in because it inspects private local sessions.

Usage means actual invocation, not mere availability in a prompt or configuration.

Invocation evidence grades:

- `strong`: session shows access to the exact `SKILL.md`, an explicit skill-use announcement, or clear execution of the skill workflow.
- `medium`: session shows commands or tool patterns strongly associated with the skill.
- `weak`: session mentions the skill name but does not show that the skill was loaded or followed.

For usage status, `unlearn` counts strong and medium evidence as used. Weak evidence is shown separately as mentioned.

History scanning is implemented through adapters:

- JSONL adapters stream records and search for actual invocation evidence.
- SQLite adapters inspect known or declared tables and scan text-like columns for actual invocation signals.

Adapters store derived evidence by default, not raw conversation excerpts.

## LLM-assisted analysis

LLM-assisted analysis is optional.

On first start, `unlearn` asks whether to enable it. If the user declines, it remains disabled until explicitly requested with `--with-llm`.

LLM-assisted analysis can provide:

- generated skill summaries
- semantic overlap groups

Generated summaries are labeled as generated and do not replace extracted summaries. Generated results are cached by skill content hash and labeled with provider/model.

## Command surface

V1 quick commands:

```bash
unlearn
```

Open the dashboard-first cleanup workbench.

```bash
unlearn audit
```

Print a concise read-only overview:

- global skill count
- finding counts
- top cleanup candidates
- pointer to `unlearn` dashboard

```bash
unlearn audit --fix
```

Apply safe quick fixes after dry-run and confirmation:

- broken reference cleanup
- broken symlink handling
- cache/index repair
- exact duplicate quarantine when confidence is 100%

Still respects trusted roots, write permissions, and action confirmations.

```bash
unlearn scan
```

Refresh the local inventory/index.

```bash
unlearn restore <skill>
```

Restore a quarantined skill.

No standalone `unlearn dedupe` or `unlearn resolve` in v1. Those flows are dashboard-only.

## Action safety

Cleanup actions operate only on skill files/directories and `unlearn`'s own state.

- `quarantine`: normal confirmation.
- `delete`: active skill deletion requires typing the skill name.
- `delete from quarantine`: normal confirmation.
- `rename`: updates both directory name and `SKILL.md` frontmatter `name`, with dry-run preview.
- `rename` on symlinked or package-managed skills: warn and suggest quarantine instead.
- batch actions: dry-run summary before execution.

V1 does not edit agent configuration files.

## Implementation stack

- Go CLI.
- Charmbracelet tooling:
  - Bubble Tea
  - Bubbles
  - Lip Gloss
  - Fang as a Charmbracelet-friendly wrapper around Cobra
- SQLite through `modernc.org/sqlite`.
- TOML for human-editable config and decisions.

## Test strategy

### Unit tests

- frontmatter parsing
- token estimation
- reference extraction
- symlink resolution
- duplicate/conflict detection
- overlap heuristics
- activation-risk heuristics
- provenance inference
- TOML decision loading/saving

### Integration tests

- fixture skill roots with duplicate/conflict/overlap cases
- trusted-root permission gates
- write permission gates
- quarantine and restore
- rename directory + frontmatter
- audit output
- `audit --fix` dry-run and confirmation behavior

### TUI model tests

- findings/skill-inventory view mode toggles
- compact/rich density toggles
- Vim and arrow key handling
- dynamic key bar action availability
- action confirmation states

## Open questions

- Whether symlinked aliases should be treated as cleanup candidates or intentional sharing by default remains unresolved.
