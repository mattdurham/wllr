---
type: Feature
title: Promote extensions to the full four-file spec treatment
description: Give extensions/ the same SPECS/NOTES/TESTS/BENCHMARKS + // NOTE invariant discipline as modules/.
status: planned
tags: [process, specs, extensions]
timestamp: 2026-07-01T13:10:47Z
branch: ""
pr: ""
started: ""
completed: ""
---

# Prompt

Extensions currently have at most a README (history/tasks/logging/permissions do;
most do not) and no // NOTE invariant. Decide whether to promote extensions to
the full spec-driven treatment used by modules/ — four living docs plus the
NOTE invariant on non-test .go files — and apply it consistently across
extensions/.

# Scope

## Packages Affected

- All [built-in](../packages/index.md) and installed extension concepts.

## Decisions to Respect

- [Spec-driven modules](../decisions/spec-driven-modules.md)

# Acceptance Criteria

- A decision recorded on whether extensions adopt the four-file treatment.
- If yes: SPECS/NOTES/TESTS present per extension and the NOTE invariant applied; AGENTS.md updated to state the rule for extensions/.
- Consistent README coverage across all extensions regardless.

# Workflow

_Filled in when work begins._
