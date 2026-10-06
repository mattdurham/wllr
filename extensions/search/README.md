# Search Extension for wllr

Structured, pageable code search that replaces raw `grep`/`rg` calls in `exec`.

## Why it exists

History analysis of real wllr sessions showed search-shaped commands are the
single largest context polluter: ~21% of `exec` calls produced ~48% of all
output bytes (multi-MB `git grep` dumps land whole in the context window), and
agents hand-rolled pagination with escalating `head -50 | head -100 | head -200`
re-runs. This extension turns search into a metered, cached, pageable tool.

## What it does

- **`search`** — runs ripgrep against the workspace, writes the full match set
  to a short-lived cache, and returns the first page plus a `result_id`,
  `total`, and `next_offset`.
- **`search_page`** — serves subsequent pages from the cached result set by
  `result_id` + `offset`, with zero re-execution.

Key properties:

- **Bounded pages** — `page_size` defaults to 50 and is capped at 200, so a
  single call can never dump megabytes into context.
- **Deterministic cache** — `result_id` is a hash of the query plus the git
  HEAD, so an identical re-query within the TTL (5 minutes) is served from
  cache (`cached: true`) instead of re-running.
- **Staleness detection** — if HEAD moved since the result set was built,
  responses carry `stale: true` so the caller knows to re-run.
- **Stream cap** — ripgrep output is capped (25 MB) and responses carry
  `complete: false` when truncated; minified files are skipped via
  `--max-columns`, long lines are clipped.
- **Scope guard** — `path` must stay inside the launch directory;
  out-of-scope paths return `path_out_of_scope` errors.

## Layout

- `main.go` (`//go:build wasip1`) — tool registration and SDK wiring only.
- `search_logic.go` (no build tag) — pure search/paging/cache logic with
  injectable `execFn`/`getenvFn`, so it is host-testable.
- `search_logic_test.go` (`//go:build !wasip1`) — host-side unit tests.
- `wllrsdk.go`, `message.go`, `pickeritem.go`, `statusinfo.go` — copies of the
  canonical SDK files from `extensions/`.

## Build and install

Built and installed by the top-level Makefile like the other optional
extensions:

```bash
make extensions   # builds + installs to ~/.wllr/extensions/search/
```

## Test

```bash
go test ./...
```
