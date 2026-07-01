Generated built-in WASM extensions are copied here by `make extensions`.

The `.wasm` files are intentionally ignored by git. `make build` runs
`make extensions` first, so release binaries embed the generated artifacts.
