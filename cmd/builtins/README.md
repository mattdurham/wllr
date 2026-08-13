Generated built-in WASM extensions are copied here by `make extensions`.

The `.wasm` files are intentionally ignored by git. `make build` runs
`make extensions` first, so release binaries embed the generated artifacts.

## Permission manifests (source of truth)

Each built-in ships a tracked permission manifest `<name>.manifest.json`
(e.g. `agents.manifest.json`, `logging.manifest.json`). These are checked in
and are the **source of truth** for what each trusted built-in may do — they are
independent of the compiled `.wasm` bytes.

This is a deliberate security boundary: if the WASM source is ever compromised,
the host still denies any host call the declared manifest does not grant.
`cmd/main.go` reads these manifests via `builtinManifestPermissions` and loads
each built-in with exactly the declared permissions (fail closed — a missing or
malformed manifest yields zero permissions, never an implicit all-permissions
grant).

Current grants (least privilege, from actual runtime host-call usage):
- `agents`, `statusline`, `plan` -> `["ui"]` (drive the TUI scene graph)
- `logging` -> `["file_write"]` (append_file)
- `history`, `queue`, `sigil` -> `[]` (unrestricted host calls only)

Keep these manifests in sync with the actual host calls each built-in makes;
`make clean` removes only the generated `.wasm` files, never these manifests.
