package extension

import (
	"context"
	"os"
	"path/filepath"
	"runtime"
	"testing"
)

// builtinWASMPaths returns paths to all .wasm files in cmd/builtins.
func builtinWASMPaths(t testing.TB) []string {
	t.Helper()
	_, file, _, _ := runtime.Caller(0)
	root := filepath.Join(filepath.Dir(file), "..")
	dir := filepath.Join(root, "cmd", "builtins")
	entries, err := os.ReadDir(dir)
	if err != nil {
		t.Skipf("builtins dir not found: %v", err)
	}
	var paths []string
	for _, e := range entries {
		if filepath.Ext(e.Name()) == ".wasm" {
			paths = append(paths, filepath.Join(dir, e.Name()))
		}
	}
	if len(paths) == 0 {
		t.Skip("no .wasm files found in cmd/builtins")
	}
	return paths
}

func loadBuiltins(ctx context.Context, t testing.TB) {
	t.Helper()
	h := NewHost(nil)
	defer h.Close(ctx)
	for _, path := range builtinWASMPaths(t) {
		data, err := os.ReadFile(path)
		if err != nil {
			t.Fatalf("read %s: %v", path, err)
		}
		if err := h.LoadBytes(ctx, filepath.Base(path), data, true); err != nil {
			t.Fatalf("load %s: %v", path, err)
		}
	}
}

// BenchmarkWASMLoad_ColdCache clears the cache before each run to measure cold compilation.
func BenchmarkWASMLoad_ColdCache(b *testing.B) {
	ctx := context.Background()
	cacheDir := "/tmp/wllr-wasm-cache"

	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		b.StopTimer()
		_ = os.RemoveAll(cacheDir)
		b.StartTimer()

		loadBuiltins(ctx, b)
	}
}

// BenchmarkWASMLoad_WarmCache pre-warms the cache then measures repeated loads.
func BenchmarkWASMLoad_WarmCache(b *testing.B) {
	ctx := context.Background()

	// Pre-warm the cache before timing starts.
	loadBuiltins(ctx, b)

	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		loadBuiltins(ctx, b)
	}
}
