package extension

// NOTE: Any changes to this file must be reflected in the corresponding SPECS.md or NOTES.md.

import (
	"context"
	"fmt"

	"github.com/tetratelabs/wazero/api"
)

// requiredExports lists the WASM exports that every extension must provide.
var requiredExports = []string{"_init", "_on_event", "_alloc", "_free"}

// validateExports returns an error if m is missing any required export.
func validateExports(m api.Module) error {
	for _, name := range requiredExports {
		if fn := m.ExportedFunction(name); fn == nil {
			return fmt.Errorf("extension missing required export: %s", name)
		}
	}
	return nil
}

// callInit calls the extension's _init() export.
// Before calling _init, the Go runtime must be bootstrapped:
//   - WASI reactor model (TinyGo): exports _initialize — call it.
//   - WASI command model (standard Go GOOS=wasip1): exports _start — call it.
//     proc_exit(0) from _start is treated as success (exit code 0).
//
// Returns an error if _init is absent or returns a non-zero status code.
func callInit(ctx context.Context, m api.Module) error {
	if fn := m.ExportedFunction("_initialize"); fn != nil {
		// WASI reactor: _initialize bootstraps the runtime without running main.
		if _, err := fn.Call(ctx); err != nil {
			return fmt.Errorf("_initialize trap: %w", err)
		}
	}
	fn := m.ExportedFunction("_init")
	if fn == nil {
		return fmt.Errorf("extension missing _init export")
	}
	results, err := fn.Call(ctx)
	if err != nil {
		return fmt.Errorf("_init trap: %w", err)
	}
	if len(results) > 0 && results[0] != 0 {
		return fmt.Errorf("_init returned error code %d", results[0])
	}
	return nil
}
