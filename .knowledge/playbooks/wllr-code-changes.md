---
type: Playbook
title: Write and verify wllr code changes
description: Repository-specific workflow for implementing Go, WASM extension, SDK, and TUI changes safely.
tags: [go, wasm, extensions, harness, sdk, testing, workflow]
generated: { by: human:mattdurham, at: 2026-08-05T00:00:00Z }
status: stable
---

Keep changes within the repository's module boundaries and preserve the
host/extension contracts.

## Before coding

1. Verify `pwd`, `git rev-parse --show-toplevel`, and `git status --short
   --branch`.
2. Read `AGENTS.md`, the relevant package `SPECS.md`, `NOTES.md`, and existing
   tests. The specification is authoritative for spec-driven modules.
3. Identify the narrowest module or extension that owns the behavior.
4. Check GitHub Issues for related work before creating duplicate scope.

## Implementation rules

- Every non-test Go file in `modules/` keeps the `// NOTE` invariant comment.
- Changes to module contracts update `SPECS.md`; non-obvious decisions update
  `NOTES.md`; changed test intent updates `TESTS.md`; performance changes update
  `BENCHMARKS.md`.
- Keep `modules/sdk` as a dependency leaf. Shared host↔WASM types belong there,
  not in harness, extension, or agent internals.
- Keep UI changes behind the harness renderer and UIBridge boundaries.
- Keep WASM extension changes compatible with `docs/extensions.md`, synchronized
  SDK boilerplate, and the extension manifest permissions.
- Update `docs/tool-contracts.md` when a tool schema or result shape changes.
- Prefer small, targeted edits. Preserve unrelated worktree changes and never
  reconstruct whole files from partial reads.

## Verification

Run the narrowest relevant checks first:

```sh
gofmt -w <changed-go-files>
go test ./path/to/touched/package
```

Then run broader checks appropriate to the change:

```sh
go test ./...
make build
```

Use `make precommit` for the repository gate before committing. For WASM
extensions, use `make builtins`, `make extensions`, or the compiler workflow
specified by the extension and verify generated artifacts deliberately. Finish
with `git diff --check`, a final diff review, and `git status --short --branch`.
