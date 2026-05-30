package extension

// NOTE: Any changes to this file must be reflected in the corresponding SPECS.md or NOTES.md.

import (
	"sync"

	"github.com/mattdurham/wllr/modules/sdk"
	"github.com/tetratelabs/wazero/api"
)

// Extension wraps a loaded WASM module.
type Extension struct {
	module        api.Module
	store         *Store
	subscriptions map[sdk.EventType]bool
	permissions   map[sdk.Permission]bool
	name          string
	subMu         sync.RWMutex
	callMu        sync.Mutex
	trusted       bool
	// Priority controls dispatch order. Lower = runs first.
	// Built-ins default to 0, user extensions to 100.
	// Within the same priority, extensions run alphabetically by name.
	Priority int
}
