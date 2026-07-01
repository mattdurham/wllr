---
type: Playbook
title: Pre-commit checks
description: The gate every change must pass before committing.
tags: [process, testing, ci]
timestamp: 2026-07-01T13:10:47Z
---

Run before every commit; a change is not done until these pass.

# Steps

1. `go test -race ./...` — full suite, race detector on.
2. `staticcheck ./...` — must be clean.
3. `go vet ./...` — must be clean.
4. `make build` — binary builds (rebuilds embedded WASM extensions).
5. For a spec-driven module change: update SPECS.md / NOTES.md / TESTS.md as required.
6. For an ABI change (host_call / event / export / permission): update docs/extensions.md in the same change.
7. Remove stray compiled extension binaries before `git add` (but keep tracked ones like extensions/lsp/lsp).

# Related

- [Spec-driven modules](../decisions/spec-driven-modules.md)
- [extension package](../packages/extension.md)
