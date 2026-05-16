# Context

## Glossary

### Skill

An instruction bundle discoverable by one or more AI coding agents. A skill usually includes a `SKILL.md` file with frontmatter and body content, plus optional scripts, references, or assets.

### Skill mess

The accumulated state where skills are spread across multiple installation roots, overlap in purpose, have unclear agent registrations, or remain installed after they stop being useful.

### Unused skill

A skill with evidence that it may no longer be worth keeping. `unlearn` treats unused as an evidence grade rather than a binary fact.

Evidence grades:

- `orphaned`: no known agent references the skill.
- `unseen`: no detected invocation or session history for the skill.
- `shadowed`: likely superseded by another similar skill.
- `kept`: explicitly protected by the user.
- `unknown`: insufficient evidence.

### Aggressive cleanup

The default cleanup stance for `unlearn`: surface more removal candidates, including low-confidence candidates, while still requiring user confirmation before destructive actions. `unlearn` does not expose separate conservative/aggressive modes in v1.

### Selective removal

A removal flow where `unlearn` asks for the action per selected skill instead of applying one global default. The user chooses whether to quarantine, delete, or skip each candidate.

### Duplicate skill

A skill that has the same name and identical effective content as another installed skill.

### Conflicting skill

A skill that has the same name as another installed skill but different effective content.

### Overlapping skill

A skill with a different name from another installed skill but a similar purpose, trigger area, or operational role.

### Token cost

An estimated range for how much context a skill may consume. The lower bound counts the skill's primary `SKILL.md`; the upper bound additionally counts referenced supporting material such as `references/*.md`, scripts, examples, or other files the skill may direct an agent to read.

### Activation risk

An estimate of how likely a skill is to be pulled into an agent context, based on breadth of frontmatter description, trigger language, and overlap with common user requests.

### Referenced support material

Supporting skill files that are explicitly linked or mentioned from `SKILL.md`. For token-cost upper bounds, `unlearn` counts referenced support material but does not blindly count every file in the skill directory. Unreferenced markdown can be surfaced separately as a warning.

### Global skill inventory

The default audit target for `unlearn`: skills installed in known global agent skill roots, such as `~/.agents/skills`, `~/.pi/agent/skills`, `~/.codex/skills`, and `~/.config/opencode/skills`. `unlearn` builds this inventory with an independent filesystem scanner rather than treating `npx skills` output as the source of truth.

### Skill root

A filesystem location that may contain one or more installed skills. `unlearn` should discover skills from known global roots and from user-provided roots. V1 has first-class support for directories containing `SKILL.md`, secondary support for standalone markdown files under known skill roots, and treats unknown shapes as skill-like items that should not be acted on destructively.

### Symlink-aware discovery

Skill discovery that records both the path encountered during scanning and the resolved real path. This allows `unlearn` to detect when two apparent installations are actually the same filesystem object.

### Skill provenance

Display-only evidence about where a skill came from, such as an npm package, GitHub repository, local git checkout, copied directory, symlink target, or agent-specific install location. Provenance helps the user decide which duplicate or overlapping skill to keep, but v1 does not use provenance to automatically rank recommendations.

### Symlinked alias

A skill installation path that resolves to the same physical directory or file content as another installation path. Whether symlinked aliases should be treated as cleanup candidates or intentional sharing is unresolved.

### Conflict resolution

A guided flow for deciding what to do when multiple skills have the same name, similar purpose, or incompatible content. The flow should help the user compare candidates and choose whether to keep one, rename one, quarantine, delete, or leave the conflict unresolved. Repointing copied duplicates to a canonical skill is out of scope for v1 because it is harder to understand and can be handled by external skill-management tools. Automated merge is out of scope for v1 because it would require delegating skill synthesis to an agent. Agent-specific variant registration is also out of scope for v1.

### Dashboard-first CLI

The primary `unlearn` experience: running `unlearn` opens a full-screen terminal dashboard for auditing, filtering, inspecting, and cleaning global skills.

### Quick command

A non-dashboard command for common focused tasks, such as producing a quick audit overview or applying safe fixes. Quick commands complement the dashboard but do not replace it. V1 quick commands include `unlearn audit`, `unlearn audit --fix`, `unlearn scan`, and `unlearn restore <skill>`. Dedupe and conflict-resolution flows remain dashboard-only in v1.

### Audit overview

The output of `unlearn audit`: a concise read-only report with aggregate counts and top cleanup candidates. It should not dump the full skill inventory by default. The report should point users to the dashboard for inspection and cleanup.

### Cleanup candidate ranking

The ordering of suggested cleanup candidates. V1 should sort candidates by deterministic finding severity and supporting reasons, but should not display a numeric cleanup score.

### Dashboard layout

The dashboard presentation for `unlearn`. It supports explicit view modes: findings view and skill inventory view. Findings view is the default and groups cleanup work by issue type; skill inventory view lists skills directly for browsing/searching. It also supports density modes: compact and rich. Compact is the default and is optimized for scanning many findings quickly; rich expands rows or details with summaries, token-cost range, activation risk, provenance, usage evidence, and actions. A dynamic key bar at the bottom shows the currently available actions and shortcuts. Navigation supports common Vim keys and arrow keys.

### Safe quick fix

An automated cleanup that has low risk and can be explained before execution. Safe quick fixes may be exposed through commands such as `unlearn audit --fix`, but destructive actions still require explicit confirmation. In v1, safe quick fixes include broken reference cleanup, broken symlink handling, cache/index repair, and exact duplicate quarantine when confidence is 100% and the user explicitly confirms.

### Finding-first dashboard

The default `unlearn` dashboard view groups work by cleanup finding, such as duplicates, conflicts, overlaps, orphaned skills, high token cost, broad activation risk, and broken symlinks. This is the primary way users decide what to clean up.

### Skill inventory view

A secondary dashboard view that lists skills directly, useful for browsing, searching, and inspecting a specific skill. A root-based view is not a v1 priority.

### Usage evidence

Read-only evidence that a skill may have actually been invoked, not merely listed as available in an agent prompt or configuration. Usage evidence can come from opt-in scans of agent session histories, shell history, or `unlearn`'s own keep/drop decisions. Historical usage scans are privacy-sensitive and must be opt-in by default.

### Actual invocation

Evidence that an agent deliberately used a skill for a task, such as reading the skill's `SKILL.md`, announcing skill use, executing a skill-provided command, or following a skill-specific workflow. A skill appearing in an available-skills list or system prompt is not actual invocation.

Invocation evidence grades:

- `strong`: session shows access to the exact `SKILL.md`, an explicit skill-use announcement, or clear execution of the skill workflow.
- `medium`: session shows commands or tool patterns strongly associated with the skill.
- `weak`: session mentions the skill name but does not show that the skill was loaded or followed.

For usage status, `unlearn` counts strong and medium evidence as used. Weak evidence is shown separately as mentioned.

### History adapter

A scanner for a specific agent history format. JSONL adapters stream records and search for evidence of actual invocation. SQLite adapters inspect known or declared tables and scan text-like columns for actual invocation signals. History adapters should store derived evidence by default, not raw conversation excerpts.

### Extracted skill summary

A deterministic explanation of what a skill does, derived from its frontmatter and body content without LLM interpretation. Extracted summaries provide the trustworthy baseline.

### Generated skill summary

An optional LLM-produced explanation of what a skill does, used to make descriptions clearer and support purpose-overlap detection. Generated summaries should be labeled as generated and should not replace the extracted baseline.

### LLM-assisted analysis

Optional analysis that uses an LLM to generate clearer skill summaries and semantic overlap groups. On first start, `unlearn` asks the user whether to enable LLM-assisted analysis. If the user declines, LLM-assisted analysis remains disabled until explicitly requested with a flag such as `--with-llm`. Generated results should be cached by skill content hash and labeled with the provider/model used.

### Local unlearn state

Persistent local state used to avoid expensive recomputation and preserve user decisions. `unlearn` should use SQLite for indexed scan results and derived history evidence, TOML for human-editable configuration and decisions, and filesystem directories for quarantined skills. Raw session excerpts are not stored by default.

### User decision

A remembered user preference such as keeping a skill, ignoring a finding, or marking a skill as a drop candidate. In v1, decisions are keyed primarily by skill name for simplicity, with enough supporting context shown in the UI to let the user revise decisions if skill content changes.

### Dashboard action

A standard action available from the dashboard. V1 actions use direct terminology: `keep`, `ignore finding`, `quarantine`, `delete`, `rename`, and `inspect`. Branded synonyms such as `forget`, `stash`, or `protect` are avoided. Rename updates both the skill directory name and `SKILL.md` frontmatter name by default, with a dry-run preview. If the skill appears symlinked or package-managed, `unlearn` should warn and suggest quarantine instead of rename.

### Destructive safety

The safety model for cleanup actions. Quarantine requires normal confirmation. Direct deletion of an active skill is allowed but requires typing the skill name. Deleting from quarantine can use normal confirmation. Batch actions show a dry-run summary before execution. V1 does not edit agent configuration files; it may report configuration issues, but cleanup actions operate on skill files/directories and `unlearn`'s own state only.

### Trusted skill root

A skill root that the user has explicitly allowed `unlearn` to scan. V1 uses a Codex-like first-time permission prompt per skill root before scanning it, and stores trust decisions in TOML. Modifying skills in a trusted root requires separate write permission before quarantine, delete, or rename. Automation can use an explicit flag such as `--trust-root <path>`.

### Charmbracelet CLI

The intended implementation style for `unlearn`: a Go CLI using Charmbracelet tooling for terminal UX. The stack may include Bubble Tea, Bubbles, Lip Gloss, and Fang as a Charmbracelet-friendly wrapper around Cobra.

### Embedded SQLite index

The local `unlearn` index should use SQLite through the pure-Go `modernc.org/sqlite` driver to avoid CGO requirements.

### TOML configuration

Human-editable `unlearn` configuration and decisions should be stored as TOML.

### First-launch setup

The initial `unlearn` experience before the dashboard opens. V1 should use one setup screen with toggles for LLM-assisted analysis and local history scanning, then build the initial global skill inventory and open the finding-first dashboard.

### Cleanup workbench

The selected v1 product direction for `unlearn`: a dashboard-first terminal workbench for auditing global skills, inspecting findings, resolving conflicts, and selectively cleaning up skill files. Quick commands complement the workbench but do not define the product.
