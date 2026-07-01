---
type: Playbook
title: Add a new WASM extension
description: Scaffold, build, and load a new Go WASM extension.
tags: [extension, wasm, authoring]
timestamp: 2026-07-01T13:10:47Z
---

# Steps

1. Create `extensions/<name>/` and copy `extensions/wllrsdk.go` into it (provides all WASM boilerplate).
2. Write `main.go`: register tools/commands in `init()`, subscribe with `On*` hooks, use host helpers.
3. Declare required permissions in the extension's JSON/YAML manifest (exec, file_*, network_*, ui).
4. Build: `GOOS=wasip1 GOARCH=wasm go build -buildmode=c-shared -o <name>.wasm .`.
5. For a **built-in**: add it to the Makefile `extensions` target → `cmd/builtins/<name>.wasm`, and `//go:embed` it in cmd/. For an **installed** extension: it builds into `~/.wllr/extensions/<name>/`.
6. If it touches the ABI, update docs/extensions.md.
7. Consider a README and host-testable logic split (untagged file + test), matching history/tasks.

# Related

- [WASM extension authoring](../patterns/wasm-extension-authoring.md)
- [Scene-graph UI](../patterns/scene-graph-ui.md)
