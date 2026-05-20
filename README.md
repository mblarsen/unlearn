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

Early v1. The core scanner, dashboard, safety gates, duplicate cleanup flows, fixture-tested mutations, opt-in history evidence, and minimal Gemini-backed LLM overlap analysis are implemented. History evidence supports opt-in JSONL files, explicitly provided SQLite databases, and SQLite databases discovered under configured roots.

## Safety model

`unlearn` is designed to avoid surprising you.

- It does not use `npx skills` output as the source of truth; it scans skill roots directly.
- It asks before scanning a skill root for the first time.
- It asks separately before writing to a trusted root.
- It does not edit agent configuration files.
- Quarantine/delete/rename actions are confirmation-gated.
- Duplicate installs require choosing the exact install, selecting multiple installs, or explicitly choosing `All N installs`.
- Restore uses a navigable quarantine list.
- History scanning is opt-in and stores derived evidence plus cache metadata, not raw session excerpts.
- LLM-assisted analysis is opt-in and falls back to deterministic analysis if no Gemini API key is configured.

Known default global roots are derived from the active agent harnesses selected in setup. The agent/root catalog is adapted from `vercel-labs/skills/src/agents.ts` and includes Pi, Codex, OpenCode, Claude Code, Cursor, Goose, Gemini CLI, and other supported Skills-compatible agents. Manual `--root` paths remain supported.

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
- Opt-in history evidence from JSONL sessions, explicit SQLite databases, and SQLite databases discovered under configured roots.
- Shared history-scan cache for JSONL and SQLite sources keyed by source fingerprint, skill set, and scanner version.
- Optional Gemini-assisted summaries and semantic overlap detection with cache-backed results.

## Install

Recommended install methods:

```bash
mise use -g "github:mblarsen/unlearn"
```

Or download the latest binary from the [GitHub releases page](https://github.com/mblarsen/unlearn/releases/latest).

### Build from source

If you want to work on `unlearn` itself, build it locally:

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
unlearn
```

If you built from source, use `./unlearn` from the repository root instead.

On first launch, `unlearn` opens setup. Choose which skill roots it may scan and whether to enable optional history or LLM-assisted analysis.

You can rerun setup later:

```bash
unlearn setup
```

For fixture or automation work, trust roots explicitly:

```bash
unlearn audit --root /tmp/test-skills --trust-root /tmp/test-skills
```

To enable minimal LLM-assisted analysis, set a Gemini API key and pass `--with-llm` or enable it in setup:

```bash
export GEMINI_API_KEY=...
unlearn audit --with-llm
```

`GOOGLE_API_KEY` is also accepted. The default model is `gemini-3-flash`; override it with `UNLEARN_LLM_MODEL`.

## Commands

```bash
unlearn
```

Open the dashboard.

```bash
unlearn audit
```

Print a concise read-only overview with skill count, finding counts, and top cleanup candidates. Add `--history-jsonl <path>` or `--history-sqlite <path>` to opt in local history evidence for unseen-skill findings. If history scanning is enabled, `unlearn` also discovers `.db`, `.sqlite`, and `.sqlite3` files under configured/trusted scan roots.

```bash
unlearn audit --with-llm
```

Opt in to Gemini-assisted summaries and semantic overlap detection. Requires `GEMINI_API_KEY` or `GOOGLE_API_KEY`; otherwise `unlearn` prints a warning and uses deterministic analysis.

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

## Roadmap

- Expand opt-in history scanning to additional agent history stores beyond Pi JSONL and configured-root SQLite discovery.
- Polish dashboard controls for history progress/cancellation.
- Expand LLM-assisted analysis beyond the minimal Gemini backend with richer provider selection and more surfaced generated summaries.
- Extend batch cleanup beyond duplicate-by-root once the interaction model is proven safe.
- Improve quarantine management with filtering, previewing, restoring, and deleting old quarantined items.
- Polish release/install paths with packaged binaries and upgrade documentation.
