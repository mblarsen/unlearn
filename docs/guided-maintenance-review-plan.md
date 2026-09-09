# Guided maintenance review plan

## Goal

Add a guided review that shows one exact installed skill at a time. Each item uses an existing duplicate, conflict, or unseen finding.

## Decisions

- Use `internal/review` as the behavior module.
- Build review items only from authoritative findings.
- Treat unseen findings as uncertain usage, not proof that a skill is unused.
- Use one exact install path as the identity of each review item.
- Save the active review scope and each decision in TOML.
- Resume an incomplete scope after a restart.
- A revisit decision skips the item only in the active scope.
- A new review includes deferred items again.
- A keep decision uses the existing logical skill-name protection.
- A quarantine decision uses the existing workbench write gate, exact target, preview, confirmation, and reconciliation.
- Do not add delete, rename, or automatic cleanup actions to this flow.
- Mark completion as completion of the saved scope only.
- Do not describe scope completion as a clean global inventory.
- Do not claim that all changes can be undone.
- Mention restore only after a successful quarantine.

## TUI flow

- Open the review with `v` from the normal dashboard.
- Show progress, finding evidence, the exact install, and the consequence of each decision.
- Use `k` to keep the logical skill name.
- Use `q` to start the existing quarantine authorization and confirmation flow.
- Use `l` to revisit the item in a later review.
- Use `Esc` to leave the review without losing saved decisions.
- Show a clear empty state when no eligible findings exist.
- Show a scoped completion state when all current items have decisions or disappeared.

## Acceptance checks

- [ ] Duplicate, conflict, and unseen findings become deterministic review items.
- [ ] Unseen evidence includes the opted-in history limitation.
- [ ] Keep and revisit decisions persist after restart.
- [ ] An incomplete review resumes at the next undecided item.
- [ ] A deferred item returns when the user starts a new review after completion.
- [ ] Quarantine uses one exact install and existing mutation safeguards.
- [ ] Partial or stale quarantine outcomes keep the inventory and review state consistent.
- [ ] Missing installs do not block review completion.
- [ ] Back and cancel preserve completed decisions.
- [ ] The TUI has usable output at 80x18, 80x24, 120x40, and 200x60.
- [ ] No raw history or new destructive shortcut is added.

## Progress

- [x] Record scope and design decisions.
- [x] Add behavior tests for the review module.
- [x] Implement review scope, decisions, resume, and completion.
- [x] Add TOML persistence tests and configuration fields.
- [x] Add TUI behavior and bounded render tests.
- [x] Connect keep, revisit, and safe quarantine actions.
- [x] Run all required validation.
