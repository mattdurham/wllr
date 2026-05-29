package extension

import (
	"sync"

	"github.com/mattdurham/wllr/sdk"
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
}
