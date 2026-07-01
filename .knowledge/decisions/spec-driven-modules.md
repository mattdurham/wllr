---
type: Decision
title: Modules are spec-driven — the // NOTE invariant
description: Every non-test .go in modules/ carries a NOTE comment; code changes must update SPECS.md/NOTES.md/TESTS.md.
tags: [process, specs, modules]
timestamp: 2026-07-01T13:10:47Z
---

**Decision:** Every non-test `.go` file in a `modules/` package carries:
`// NOTE: Any changes to this file must be reflected in the corresponding SPECS.md or NOTES.md.`
Each module keeps four living docs — SPECS.md (contracts/invariants), NOTES.md
(dated design decisions, append-only), TESTS.md (test specs), BENCHMARKS.md.

**Rationale:** Keeps the source of truth for behavior and rationale next to the
code and current, so contributors and agents can trust the docs.

**Consequence:** A code change in a spec-driven module must update the relevant
spec files in the same change; new files get the NOTE comment. Pure refactors
with no behavior change are exempt.

# Applies To

- All [core packages](../packages/index.md) under modules/.

# Origin

AGENTS.md — "Spec-Driven Development".
