#!/usr/bin/env bash
set -eu

usage() {
	cat >&2 <<'USAGE'
Usage: scripts/build-wasm-extension.sh <output.wasm> <extension-dir>

Environment:
  WASM_COMPILER  auto|tinygo|go  default: auto
  WASM_COMPILER_MANIFEST         default: build/wasm-compilers.tsv
  TINYGO_MODE    docker|local     default: docker
  TINYGO         TinyGo binary    default: tinygo
  TINYGO_IMAGE   Docker image     default: tinygo/tinygo:latest
  TINYGO_FLAGS   TinyGo flags     default: -buildmode=c-shared -target=wasi -opt=z

auto uses the per-extension compiler decision in this script. There is no
automatic fallback: an extension either supports TinyGo or is explicitly built
with standard Go until it is ported.
USAGE
}

if [ "$#" -ne 2 ]; then
	usage
	exit 2
fi

output=$1
extension_dir=$2
compiler=${WASM_COMPILER:-auto}
tinygo_mode=${TINYGO_MODE:-docker}
tinygo_bin=${TINYGO:-tinygo}
tinygo_image=${TINYGO_IMAGE:-tinygo/tinygo:latest}
tinygo_flags=${TINYGO_FLAGS:--buildmode=c-shared -target=wasi -opt=z}
repo_root=$(git rev-parse --show-toplevel 2>/dev/null || pwd)
manifest=${WASM_COMPILER_MANIFEST:-build/wasm-compilers.tsv}

case "$compiler" in
auto | tinygo | go) ;;
*)
	echo "build-wasm-extension: invalid WASM_COMPILER=$compiler (want auto, tinygo, or go)" >&2
	exit 2
	;;
esac

case "$tinygo_mode" in
docker | local) ;;
*)
	echo "build-wasm-extension: invalid TINYGO_MODE=$tinygo_mode (want docker or local)" >&2
	exit 2
	;;
esac

if [ ! -d "$extension_dir" ]; then
	echo "build-wasm-extension: extension directory not found: $extension_dir" >&2
	exit 2
fi

mkdir -p "$(dirname "$output")"
output_dir=$(cd "$(dirname "$output")" && pwd)
output_abs="$output_dir/$(basename "$output")"

build_go() {
	echo "wasm: building $extension_dir with go -> $output"
	(
		cd "$extension_dir" &&
			GOOS=wasip1 GOARCH=wasm go build -buildmode=c-shared -o "$output_abs" .
	)
}

build_tinygo() {
	case "$tinygo_mode" in
	docker)
		if ! command -v docker >/dev/null 2>&1; then
			echo "build-wasm-extension: Docker required for TinyGo build but not found" >&2
			exit 1
		fi
		output_base=$(basename "$output_abs")
		output_mount=$(dirname "$output_abs")
		echo "wasm: building $extension_dir with tinygo docker ($tinygo_image) -> $output"
		# shellcheck disable=SC2086
		docker run --rm \
			-u "$(id -u):$(id -g)" \
			-e HOME=/tmp \
			-v "$repo_root:/src" \
			-v "$output_mount:/out" \
			-w "/src/$extension_dir" \
			"$tinygo_image" \
			tinygo build $tinygo_flags -o "/out/$output_base" .
		;;
	local)
		if ! command -v "$tinygo_bin" >/dev/null 2>&1; then
			echo "build-wasm-extension: TinyGo required but not found: $tinygo_bin" >&2
			exit 1
		fi
		echo "wasm: building $extension_dir with local tinygo -> $output"
		# shellcheck disable=SC2086
		(
			cd "$extension_dir" &&
				"$tinygo_bin" build $tinygo_flags -o "$output_abs" .
		)
		;;
	esac
}

selected_compiler() {
	awk -v ext="$extension_dir" '
		$0 ~ /^[[:space:]]*#/ || $0 ~ /^[[:space:]]*$/ { next }
		$1 == ext { print $2; found=1; exit }
		END { if (!found) exit 1 }
	' "$repo_root/$manifest"
}

if [ "$compiler" = auto ]; then
	if ! compiler=$(selected_compiler); then
		echo "build-wasm-extension: no compiler decision for $extension_dir in $manifest" >&2
		exit 2
	fi
	if [ "$compiler" != tinygo ] && [ "$compiler" != go ]; then
		echo "build-wasm-extension: invalid compiler decision for $extension_dir: $compiler" >&2
		exit 2
	fi
	echo "wasm: selected $compiler for $extension_dir"
fi

case "$compiler" in
go)
	build_go
	;;
tinygo)
	build_tinygo
	;;
esac
