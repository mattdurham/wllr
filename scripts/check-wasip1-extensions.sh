#!/usr/bin/env bash
set -eu

usage() {
	cat >&2 <<'USAGE'
Usage: scripts/check-wasip1-extensions.sh

Compile-check every WASM extension under extensions/ for GOOS=wasip1 GOARCH=wasm.

This is a compile-only guard, not a WASM build. It needs neither TinyGo nor
Docker, so it runs in CI. Its job is to catch the failure mode where a
//go:build wasip1 file references a symbol that was never committed: native
`go build ./...` and `go test ./...` skip wasip1 files entirely, so that class
of breakage is invisible to every other check.

It also typechecks wasip1-tagged test files (via `go vet`), which `go test` on a
non-wasm host never compiles.

`go vet` runs with -unsafeptr=false because the wasip1 SDK legitimately passes
unsafe.Pointer across the host-function ABI; every other vet check is enabled.

Exit status is 0 when all extensions compile, 1 when any fail.
USAGE
}

if [ "${1:-}" = "-h" ] || [ "${1:-}" = "--help" ]; then
	usage
	exit 0
fi

if [ "$#" -ne 0 ]; then
	usage
	exit 2
fi

repo_root=$(git rev-parse --show-toplevel 2>/dev/null || pwd)
cd "$repo_root"

tmpdir=$(mktemp -d)
trap 'rm -rf "$tmpdir"' EXIT

# Indent captured compiler output so it reads as part of the FAIL line.
indent() {
	while IFS= read -r line; do
		printf '        %s\n' "$line"
	done <"$1"
}

failures=0
checked=0

for dir in extensions/*/; do
	# Only directories that actually hold Go source. extensions/memory/ is a
	# build-artifact directory and has none.
	if ! compgen -G "$dir*.go" >/dev/null; then
		continue
	fi

	name=${dir%/}
	checked=$((checked + 1))
	artifact="$tmpdir/$(basename "$dir").wasm"

	# Extensions are a mix of nested modules (own go.mod, must build from their
	# own directory) and packages of the main module (must build from the root,
	# or the main module cannot resolve them).
	if [ -f "$dir/go.mod" ]; then
		build_dir=$dir
		build_pkg=.
	else
		build_dir=.
		build_pkg="./$dir"
	fi

	if ! (cd "$build_dir" && GOOS=wasip1 GOARCH=wasm go build -o "$artifact" "$build_pkg") 2>"$tmpdir/build.err"; then
		echo "FAIL  $name (go build)"
		indent "$tmpdir/build.err"
		failures=$((failures + 1))
		continue
	fi

	if ! (cd "$build_dir" && GOOS=wasip1 GOARCH=wasm go vet -unsafeptr=false "$build_pkg") 2>"$tmpdir/vet.err"; then
		echo "FAIL  $name (go vet)"
		indent "$tmpdir/vet.err"
		failures=$((failures + 1))
		continue
	fi

	echo "OK    $name"
done

echo ""
if [ "$failures" -gt 0 ]; then
	echo "wasip1 check FAILED: $failures of $checked extension(s) did not compile."
	exit 1
fi

echo "wasip1 check passed: all $checked extension(s) compile for wasip1."
