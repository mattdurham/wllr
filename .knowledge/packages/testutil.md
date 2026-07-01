---
type: Go Package
title: testutil
description: Test helpers — fake language model and provider — for exercising agent/harness/session without a real LLM.
resource: ./modules/testutil
tags: [testing, fakes, mock-provider]
timestamp: 2026-07-01T13:10:47Z
---

The `testutil` package provides a fake `fantasy` language model and provider so
tests can drive turns deterministically without network calls. Used across the
agent, harness, and session test suites.

# Specification

- [Contracts and invariants](../../modules/testutil/SPECS.md)
- [Design decisions](../../modules/testutil/NOTES.md)
- [Test plan](../../modules/testutil/TESTS.md)

# Dependencies

- `sdk`, `charm.land/fantasy`
