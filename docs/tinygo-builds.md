# TinyGo WASM Extension Builds

wllr extension builds use an explicit per-extension compiler decision. TinyGo
produces smaller WASI modules, and the extension loader already supports
TinyGo's `_initialize` export before calling the wllr `_init` ABI entry point.

The build mode is controlled by `WASM_COMPILER`:

```sh
make build                    # auto: use the per-extension compiler decision
WASM_COMPILER=tinygo make build
WASM_COMPILER=go make build
```

`WASM_COMPILER=auto` is the default. It reads
`build/wasm-compilers.tsv` and does not dynamically fall back. Each extension is
either marked TinyGo-compatible or built with standard Go until it is ported. If
the selected compiler is missing or fails, the build fails.

All built-in and installed extensions used by `make build` and
`make extensions` are currently selected for TinyGo in `auto` mode. Keep future
extensions on standard Go until their SDK/ABI usage is ported and a Docker
TinyGo build is verified. Standard Go uses:

```sh
GOOS=wasip1 GOARCH=wasm go build -buildmode=c-shared -o extension.wasm .
```

Use `WASM_COMPILER=tinygo` in local experiments to force TinyGo across all
extensions and expose incompatibilities. Use `WASM_COMPILER=go` to force the
previous standard Go WASI build path.

TinyGo settings:

```sh
TINYGO_MODE=docker
TINYGO_IMAGE=tinygo/tinygo:latest
TINYGO=tinygo
TINYGO_FLAGS="-buildmode=c-shared -target=wasi -opt=z"
```

Docker mode is the default so contributors do not need a host TinyGo install.
Use `TINYGO_MODE=local` only when intentionally testing a local TinyGo toolchain.
The default image is `tinygo/tinygo:latest`; `build/tinygo-builder.Dockerfile`
provides a project-local wrapper image if we need to pin packages or add helper
tools later:

```sh
docker build -f build/tinygo-builder.Dockerfile -t wllr-tinygo .
TINYGO_IMAGE=wllr-tinygo WASM_COMPILER=tinygo make builtins
```

The default target matches TinyGo's WASI support and wllr's wazero host. TinyGo
documents WASI builds with `GOOS=wasip1 GOARCH=wasm tinygo build -o main.wasm`
and supports both WASI Preview 1 and Preview 2; wllr currently consumes Preview
1 style WASI modules.

The helper script is `scripts/build-wasm-extension.sh`. All built-in and
optional extension Makefile recipes call it so compiler selection stays
consistent.

## Comparing TinyGo And Go

Use `scripts/compare-wasm-compilers.sh` to build both variants and compare
artifact sizes plus `wllr -exec` wall time and maximum resident set size:

```sh
ITERATIONS=5 scripts/compare-wasm-compilers.sh
```

The script builds isolated TinyGo and Go binaries because built-in WASM
extensions are embedded into `dist/wllr` at build time. It also gives each
variant its own temporary `HOME` under `dist/compare-wasm/` so installed
extensions do not cross-contaminate the runtime comparison.
