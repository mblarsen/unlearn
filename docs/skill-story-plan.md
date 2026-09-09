# Skill story plan

Status: complete

## Goal

Add a read-only story for each logical skill in the dashboard.

The story must show observed facts and explicit evidence limits. It must not infer origin, install dates, or upstream modification status from file timestamps.

## Decisions

- Add an `internal/story` module with one build interface for grouped installs and usage coverage.
- Treat usage evidence as skill-name evidence. Do not attribute it to one installed copy.
- Compare installed copies against the first exact path in deterministic path order.
- Compare observed metadata, body content, support references, and effective content hashes.
- If no upstream baseline exists, say that the baseline and modification status are unknown.
- If only one copy exists, say that no installed copy comparison is available.
- Open the story with Enter from the skill inventory.
- Use a scrollable full-screen modal. Use `j`/`k`, arrow keys, Page Up, and Page Down.
- Close the story with Escape and return to the same selected skill.
- Keep existing constructors compatible. They use unknown history coverage.
- Pass audit evidence coverage from the dashboard load path through a new additive constructor.

## Acceptance checks

- [x] Show every exact installed path and resolved symlink target.
- [x] Show known root ownership, agent access, and source-layout evidence.
- [x] Label unknown origin, install date, upstream baseline, and modification status.
- [x] Explain differences between installed copies without claiming upstream modification.
- [x] Show metadata, body, support-reference, and effective-content comparisons.
- [x] Show opt-in derived usage grade, last seen time, source count, and coverage.
- [x] Explain that not observed does not mean unused.
- [x] Explain that name-level usage cannot identify an invoked installed copy.
- [x] Keep all story content reachable at 80×18, 80×24, 120×40, and 200×60.
- [x] Preserve resize, back, cancel, help, and existing dashboard actions.
- [x] Use only temporary files and synthetic inventory data in tests.

## Validation

- [x] `go test ./... -count=1`
- [x] `go test -race ./internal/story ./internal/tui ./cmd/unlearn -count=1`
- [x] `go build .`
- [x] `mise run check`
