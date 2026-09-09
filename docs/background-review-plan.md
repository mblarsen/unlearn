# Background LLM review plan

## Goal

Open the dashboard after local inventory, usage evidence, and deterministic analysis complete. Continue optional LLM review sequentially in the dashboard without blocking input.

## Test seams

- `internal/analysis`: stream typed summary, finding, progress, failure, and completion events from one sequential LLM review; continue safe independent work after individual failures and stop on context cancellation.
- `internal/audit`: preserve synchronous `Run` semantics for commands; prepare a dashboard result without network work and expose an optional background review only when policy and cache provenance require it.
- `internal/tui`: consume controlled background events through Bubble Tea commands; keep input/modal/cursor state, accept only exact installs with unchanged content hashes, persist only the reconciled current snapshot, and expose bounded navigable redacted failures.
- `cmd/unlearn`: start the dashboard before a blocked analyzer completes and cancel its review context on exit.

## Implementation

- [x] Add failing regression tests for sequential incremental review, partial failure, and cancellation.
- [x] Add failing dashboard model tests for progress, incremental findings, stale-result rejection, mutation-safe persistence, state preservation, errors, and supported viewports.
- [x] Add failing command/audit tests for non-blocking dashboard preparation, cache/privacy policy, and synchronous command parity.
- [x] Implement the smallest typed review event stream behind analysis and audit interfaces.
- [x] Wire the stream into the TUI lifecycle and root command with cancellation.
- [x] Run focused tests, `go test ./... -count=1`, relevant race tests, `go build -o /tmp/unlearn-background-review .`, and `mise run check`.
- [x] Record validation results and known gaps before requesting independent review.

## Independent review fixes

- [x] Clone the background review base so TUI summary updates cannot race worker reads.
- [x] Preserve selected finding and exact focused install when incremental findings reorder grouped sections.
- [x] Add an advertised `e` route for LLM diagnostics in findings, skills, guided review, and collections that works despite prior status, without capturing normal Enter behavior or later action errors.
- [x] Add permanent regressions and pass the independent reviewer's race-overlay probes, including the follow-up Enter-capture probe.

## Validation

- `go test ./... -count=1` passed.
- `go test -race ./internal/analysis ./internal/audit ./internal/tui ./cmd/unlearn -count=1` passed.
- `go test -race -overlay /tmp/unlearn-review-overlay.json ./internal/tui -run TestIndependent -count=1` passed after the review fixes.
- `go build -o /tmp/unlearn-background-review .` passed.
- `mise run check` passed.
- Tests use controlled analyzers, temporary roots, and temporary SQLite state. No live API, credentials, user skills, config, or history were used.

## Known gaps

- No live Gemini or manual terminal session was run by design; deterministic tests cover blocked requests, cancellation, partial failures, persistence, and 80×18, 80×24, 120×40, and 200×60 rendering.
- Parallel requests and overlap request splitting remain out of scope.
