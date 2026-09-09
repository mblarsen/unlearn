# Task-based collections plan

## Goal

Add named collections that organize exact installed skills without changing skill files, agent access, or activation.

## Decisions

- Store collections in the existing TOML config.
- Identify each member by its cleaned encountered install path.
- Store the skill name with the path so stale members stay understandable.
- Never match a stale path to a different install by name.
- Treat agent availability as inventory evidence only. Do not claim that an agent loads or invokes a skill.
- Make suggestions deterministic from observed skill names and descriptions.
- Require manual acceptance for every suggestion.
- Do not install, activate, remove, quarantine, or edit a skill through collection actions.

## Module

Create a deep `internal/collections` module with one command interface. The module owns validation, CRUD, exact membership, availability previews, stale-state handling, and suggestion ordering.

The TUI uses an optional collection capability on its action adapter. Existing action fakes do not need collection methods.

## Acceptance checks

- [x] Create, rename, and delete a named collection.
- [x] Add and remove an exact installed skill membership.
- [x] Persist collections across config reloads.
- [x] Preserve all unrelated config values during collection changes.
- [x] Delete only collection organization, never installed skill files.
- [x] Keep stale members visible and removable after an install disappears.
- [x] Do not silently retarget stale membership to another copy.
- [x] Preview active and inactive agent visibility from inventory evidence.
- [x] Explain divergent copies without treating them as one install.
- [x] Suggest skills from a project or task with deterministic reasons.
- [x] Require manual acceptance before a suggestion becomes a member.
- [x] Support create, rename, delete, add, remove, suggest, back, and help in the TUI.
- [x] Keep content bounded at 80x18, 80x24, 120x40, and 200x60.
- [x] Cover empty inventory, empty collections, stale members, Unicode names, restart persistence, and cancel behavior.

## Validation

- [x] Run focused package and TUI tests.
- [x] Run `go test ./... -count=1`.
- [x] Run relevant race tests.
- [x] Run `go build .`.
- [x] Run `mise run check`.
