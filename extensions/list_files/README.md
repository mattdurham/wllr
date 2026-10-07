# list_files Extension for wllr

Structured, pageable, gitignore-aware file listing that replaces raw
`find`/`ls` calls in `exec`.

## Why it exists

History analysis of real wllr sessions showed `find`/`ls` were ~12% of all
`exec` calls, frequently hand-capped with escalating `| head -25 | head -100 |
head -200` re-runs that hid whether the listing was actually complete. This
extension turns directory listing into a metered, cached, pageable tool.

## What it does

- **`list_files`** — runs `rg --files` against the workspace, writes the full
  entry set to a short-lived cache, and returns the first page plus a
  `result_id`, `total`, and `next_offset`.
- **`list_files_page`** — serves subsequent pages from the cached result set by
  `result_id` + `offset`, with zero re-walk.

Key properties:

- **Bounded pages** — `page_size` defaults to 200 and is capped at 1000, so a
  single call can never dump a huge tree into context.
- **Gitignore-aware** — `rg --files` respects `.gitignore`; hidden files/dirs
  are skipped unless `hidden: true`.
- **Explicit completeness** — responses carry `truncated`/`complete` so a
  stream-capped listing is never mistaken for a full one.
- **Entry types** — `type` selects `files` (default), `dirs`, or `both`.
  Directories are returned with a trailing `/` so they are visually distinct.
- **Glob + depth filters** — `glob` (ripgrep syntax, e.g. `*.go`) and
  `max_depth` narrow the walk.
- **Deterministic cache** — `result_id` is a hash of the query plus the git
  HEAD, so an identical re-query within the TTL (5 minutes) is served from
  cache (`cached: true`) instead of re-walking.
- **Staleness detection** — if HEAD moved since the entry set was built,
  responses carry `stale: true` so the caller knows to re-run.
- **Scope guard** — `path` must stay inside the launch directory;
  out-of-scope paths return `path_out_of_scope` errors.

## Layout

- `main.go` (`//go:build wasip1`) — tool registration and SDK wiring only.
- `list_files_logic.go` (no build tag) — pure listing/paging/cache logic with
  injectable `execFn`/`getenvFn`, so it is host-testable.
- `list_files_logic_test.go` — host-side unit tests.
- `wllrsdk.go`, `message.go`, `pickeritem.go`, `statusinfo.go` — copies of the
  canonical SDK files from `extensions/`.

## Build and install

Built and installed by the top-level Makefile like the other optional
extensions:

```bash
make extensions   # builds + installs to ~/.wllr/extensions/list_files/
```

## Test

```bash
go test ./...
```
