#!/usr/bin/env bash
set -eu

iters=${ITERATIONS:-3}
prompt=${PROMPT:-Reply with OK only.}
provider=${WLLR_PROVIDER:-local}
model=${WLLR_MODEL:-qwen/qwen3-coder-next}
local_base_url=${WLLR_LOCAL_BASE_URL:-http://localhost:1234/v1}
local_context_window=${WLLR_LOCAL_CONTEXT_WINDOW:-262144}
out_dir=${OUT_DIR:-dist/compare-wasm}

run_make() {
	echo "+ $*"
	"$@"
}

size_bytes() {
	if stat -f%z "$1" >/dev/null 2>&1; then
		stat -f%z "$1"
	else
		stat -c%s "$1"
	fi
}

build_variant() {
	compiler=$1
	name=$2
	variant_dir="$out_dir/$name"
	home_dir="$variant_dir/home"

	rm -rf "$variant_dir"
	mkdir -p "$variant_dir/builtins" "$home_dir/.wllr/extensions"

	run_make make build WASM_COMPILER="$compiler"
	cp dist/wllr "$variant_dir/wllr"
	cp cmd/builtins/*.wasm "$variant_dir/builtins/"

	run_make make optional-extensions WASM_COMPILER="$compiler" EXT_DIR="$home_dir/.wllr/extensions"
}

report_sizes() {
	name=$1
	variant_dir="$out_dir/$name"
	echo
	echo "== $name sizes =="
	size_bytes "$variant_dir/wllr" | awk '{ printf "binary\t%s bytes\n", $1 }'
	find "$variant_dir/builtins" "$variant_dir/home/.wllr/extensions" -name '*.wasm' -type f -print |
		sort |
		while IFS= read -r f; do
			printf "%s\t%s bytes\n" "${f#"$variant_dir"/}" "$(size_bytes "$f")"
		done
}

bench_variant() {
	name=$1
	variant_dir="$out_dir/$name"
	home_dir="$variant_dir/home"

	echo
	echo "== $name runtime =="
	for i in $(seq 1 "$iters"); do
		log="$variant_dir/run-$i.log"
		time_log="$variant_dir/time-$i.log"
		/usr/bin/time -l -o "$time_log" \
			env HOME="$home_dir" \
			WLLR_PROVIDER="$provider" \
			WLLR_MODEL="$model" \
			WLLR_LOCAL_BASE_URL="$local_base_url" \
			WLLR_LOCAL_CONTEXT_WINDOW="$local_context_window" \
			"$variant_dir/wllr" -exec "$prompt" >"$log" 2>&1
		wall=$(awk '/real/ { print $1 }' "$time_log")
		rss=$(awk '/maximum resident set size/ { print $1 }' "$time_log")
		ready=$(grep -m1 'extensions ready' "$log" || true)
		printf "run %s\twall=%ss\tmax_rss=%s bytes\t%s\n" "$i" "$wall" "$rss" "$ready"
	done
}

mkdir -p "$out_dir"

build_variant tinygo tinygo
build_variant go go

report_sizes tinygo
report_sizes go

bench_variant tinygo
bench_variant go
