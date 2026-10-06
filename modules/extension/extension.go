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
	// callMu serializes every export invocation on the module (_init,
	// _on_event, _alloc, _free), including the re-entrant _alloc the host
	// performs to return a host_call response. TinyGo's asyncify scheduler
	// state machine is not re-entrant: two goroutines inside one module
	// corrupt it and the next export call traps "unreachable". Held across
	// _init during load and across each dispatchToExtension; host_call
	// handlers always run on the goroutine that already holds it.
	callMu  sync.Mutex
	trusted bool
	// Priority controls dispatch order. Lower = runs first.
	// Built-ins default to 0, user extensions to 100.
	// Within the same priority, extensions run alphabetically by name.
	Priority int
}
