# unlearn

`unlearn` is a safety-first terminal workbench for auditing and cleaning up AI-agent skills.

It helps answer questions like:

- Which skills are installed globally?
- Which skills are duplicated across agent roots?
- Which skills overlap in purpose?
- Which skills are expensive to load into context?
- Which skills look unused or stale?
- What exactly will be removed before anything destructive happens?

The default experience is a full-screen Charmbracelet dashboard. Quick commands exist for fast audits and scripted refreshes.

## Status

Early v1. The core scanner, dashboard, safety gates, duplicate cleanup flows, and fixture-tested mutations are implemented. LLM-assisted analysis and SQLite history adapters are intentionally still limited.

## Safety model

`unlearn` is designed to avoid surprising you.

- It does not use `npx skills` output as the source of truth; it scans skill roots directly.
- It asks before scanning a skill root for the first time.
- It asks separately before writing to a trusted root.
- It does not edit agent configuration files.
- Quarantine/delete/rename actions are confirmation-gated.
- Duplicate installs require choosing the exact install, selecting multiple installs, or explicitly choosing `All N installs`.
- Restore uses a navigable quarantine list.
- History scanning is opt-in and stores derived evidence, not raw session excerpts.
- LLM-assisted analysis is opt-in.

Known default global roots:

```text
~/.agents/skills
~/.pi/agent/skills
~/.codex/skills
~/.config/opencode/skills
```

## Features

- Finding-first dashboard grouped by:
  - duplicates
  - conflicts
  - overlaps
  - high token cost
  - broad activation risk
  - unseen skills
  - broken symlinks/references
- Skill inventory view with logical skills consolidated across installs.
- `SKILL.md` frontmatter/body parsing.
- Standalone markdown skill support.
- Symlink-aware inventory.
- Token-cost estimates from `SKILL.md` plus explicitly referenced support files.
- Provenance display for install/source clues.
- Generic-word filtering to avoid noisy overlap findings.
- Width-aware keybar and modal action flows.
- Quarantine/restore support.
- Batch duplicate cleanup by root.

## Install from source

```bash
git clone git@github.com:mblarsen/unlearn.git
cd unlearn
mise install
mise run build
```

The local binary is written to:

```text
./unlearn
```

You can also run without building:

```bash
mise exec -- go run .
```

## First run

Open the dashboard:

```bash
./unlearn
```

On first launch, `unlearn` opens setup. Choose which skill roots it may scan and whether to enable optional history or LLM-assisted analysis.

You can rerun setup later:

```bash
./unlearn setup
```

For fixture or automation work, trust roots explicitly:

```bash
./unlearn audit --root /tmp/test-skills --trust-root /tmp/test-skills
```

## Commands

```bash
unlearn
```

Open the dashboard.

```bash
unlearn audit
```

Print a concise read-only overview with skill count, finding counts, and top cleanup candidates.

```bash
unlearn audit --fix
```

Preview safe fixes and apply only after confirmation. Safe fixes include broken references/symlinks, cache/index repair, and exact duplicate quarantine when confidence is high.

```bash
unlearn scan
```

Refresh the local SQLite index.

```bash
unlearn restore <skill> --to-root <root>
```

Restore a quarantined skill into a write-enabled root.

## Dashboard shortcuts

Navigation:

```text
j/k or arrows    move
s                skill inventory
f                findings
r                compact/rich density
tab              next focused install
shift+tab        previous focused install
q                quit
```

Actions:

```text
ctrl+k           keep skill
ctrl+g           ignore finding
ctrl+q           quarantine
ctrl+d           delete
ctrl+r           rename
ctrl+u           restore / undo
ctrl+b           batch duplicate cleanup by root
```

`ctrl+g` is used for ignore because many terminals report `ctrl+i` as Tab.

## Development

Tooling is managed through `mise.toml`.

```bash
mise install
mise run setup
mise run check
mise exec -- go test ./...
mise exec -- go build ./...
```

Useful tasks:

```bash
mise run fmt
mise run build
mise run goreleaser-check
mise run release-snapshot
```

Pre-commit includes Go imports, TruffleHog, Commitizen, and basic file hygiene checks.

## Design notes

The main product spec lives in:

```text
docs/superpowers/specs/2026-05-17-unlearn-skill-cleanup-workbench-design.md
```

The implementation checklist and QA notes live in:

```text
docs/implementation-plan.md
```

Domain language is captured in:

```text
CONTEXT.md
```

## Current limitations

- LLM-assisted summaries/overlap analysis are represented by an opt-in stub.
- SQLite history adapters are not implemented yet.
- Pi history discovery is bounded to known JSONL session locations.
- Batch cleanup is specialized for duplicate installs by root rather than arbitrary multi-select across all finding types.
