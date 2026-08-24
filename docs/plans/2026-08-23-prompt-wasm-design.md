---
type: Plan
title: Move prompt assembly into a built-in WASM extension
description: Design for configurable, composable prompt assembly owned by prompt.wasm.
tags: [prompt, wasm, extensions, configuration, context]
status: approved
---

# Prompt WASM Design

## Goal

Move the built-in system prompt and startup context loading from native Go and
the harness into a bundled `prompt.wasm` extension. Keep prompt components
composable: the prompt extension owns the complete base prompt, while other
extensions may append their own sections.

## Architecture

`prompt.wasm` subscribes to `session_start`, reads the `wllr` configuration
through the existing `config_read` capability, reads configured files and
AGENTS/CLAUDE context through WASI, and calls `SetSystemPrompt` exactly once.
The `session_start` payload carries the current registered tool and command
catalog so the extension can generate the dynamic action section. The harness
no longer assembles prompt text.

Prompt order is built-in text (or `prompt_override`), configured files, global
context, nearest project context, and the current working-directory note.
Other extensions can call `AppendSystemPrompt` after this base is installed.

## Configuration

The `wllr` config group accepts:

```json
{
  "prompt_override": "plain text replacing the built-in prompt",
  "prompt_files": ["docs/agent-guidance.md", ".wllr/project-prompt.md"]
}
```

Relative paths resolve from the launch directory; absolute paths and leading
`~` are accepted. Missing or unreadable optional prompt files are logged and
skipped.

## Error handling and security

The built-in prompt extension receives only `file_read` permission. Prompt
assembly errors are non-fatal where the content is optional. The host keeps
the existing config and file capability implementations; no new host call is
needed.

## Testing

Tests cover override precedence, configured file order, context lookup,
dynamic tool/command rendering, and preservation of later extension appends.
Build verification includes the prompt WASM artifact, touched Go packages,
all Go tests, diff checks, and final worktree inspection.
