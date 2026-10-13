package harness

// NOTE: Any changes to this file must be reflected in the corresponding SPECS.md or NOTES.md.

import (
	"strings"
	"sync"
	"time"

	"github.com/mattdurham/wllr/modules/sdk"
)

// tokenBatcher coalesces assistant text into batches delivered to a
// renderer-neutral token sink. The sink is an interface rather than a concrete
// front end so the batcher is driven identically by the TUI (teaSink), the HTTP
// server (HubSink), or a programmatic embedder, and can be captured headlessly
// by tests without a running TUI.
type tokenBatcher struct {
	lastSend time.Time
	sink     sdk.TokenSink
	// dispatch, when non-nil, is called with each flushed batch of text so the
	// batch can be forwarded to WASM extensions (EventToken). Called on the
	// agent goroutine, not the bubbletea loop.
	dispatch func(string)
	buf      strings.Builder
	mu       sync.Mutex
}
