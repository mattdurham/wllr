---
type: Decision
title: Agents verify repository state before editing
description: Coding agents must establish the repository root, preserve user changes, make targeted edits, and verify results before reporting completion.
tags: [agent, workflow, editing, verification, safety]
generated: { by: human:mattdurham, at: 2026-08-05T00:00:00Z }
status: stable
---

Coding agents work from verified repository state rather than inferred paths or
remembered file contents. This is especially important when a model can access
multiple worktrees or when a tool returns incomplete output.

## Before editing

Run:

```sh
pwd
git rev-parse --show-toplevel
git status --short --branch
```

Use the reported repository root and inspect the target file, its tests, and its
authoritative specification before changing behavior or documentation.

## During editing

- Make the smallest targeted change that satisfies the request.
- Preserve unrelated worktree changes.
- Never reconstruct or replace a complete source/spec file from a truncated
  read or an inferred schema.
- Do not use destructive checkout, reset, or clean commands to discard changes
  without explicit authorization for the exact target.

## Before reporting completion

Inspect the final diff and run the narrowest relevant tests first, followed by
broader checks when the change warrants them. Always run `git diff --check` and
confirm the final branch/status. Treat failed or empty tool output as unknown;
retry against an authoritative source before concluding that a file, issue,
branch, or feature is absent. Report external writes only when the command or
connector confirms them, including the commit SHA, issue number, or URL.
