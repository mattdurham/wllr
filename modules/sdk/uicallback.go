package sdk

// NOTE: Any changes to this file must be reflected in the corresponding SPECS.md or NOTES.md.

import (
	"context"
	"errors"
	"strings"
)

// CoreCallbackPrefix marks a UI callback that belongs to the engine harness
// rather than to an extension. The interactive harness routes these to its own
// handlers (model/thinking/speed selection, auth, local-model setup) instead of
// dispatching EventOnCommand. Every front end must make the same split, so the
// rule lives here as the single source of truth.
const CoreCallbackPrefix = "__wllr:"

// IsCoreCallback reports whether callback is a core-owned harness callback.
func IsCoreCallback(callback string) bool {
	return strings.HasPrefix(callback, CoreCallbackPrefix)
}

// ErrNoCallbackRouter is returned when a callback reaches a front end that has
// no router configured at all.
var ErrNoCallbackRouter = errors.New("no UI callback router configured")

// ErrNoCoreHandler is returned when a core-owned callback reaches a router that
// has no core handler installed. It is distinct from a transport failure so a
// server can answer "not implemented" rather than "error".
var ErrNoCoreHandler = errors.New("no core callback handler")

// ErrNoExtensionHost is returned when an extension-owned callback reaches a
// router that has no extension host to dispatch it to.
var ErrNoExtensionHost = errors.New("no extension host for callback")

// UICallbackRouter resolves a UI callback that a front end cannot handle
// itself: a picker or text-input selection, or an agent-tree node action, whose
// callback id names either an extension command or a core engine action.
//
// It is the mirror image of UISink. The sink carries effects out to a front
// end; the router carries a front end's choice back into the engine. A TUI
// resolves callbacks in its own event loop; a browser or an embedder has no
// such loop, so it POSTs the callback and lets a router do the dispatch.
//
// Implementations MUST be safe for concurrent use and MUST NOT block
// indefinitely.
type UICallbackRouter interface {
	// RouteCallback dispatches callback with args (for a picker, the selected
	// item id; for a text input, the entered value). It returns a non-nil error
	// when the callback is unknown or the dispatch failed; callers should treat
	// ErrNoCoreHandler and ErrNoExtensionHost as "unsupported here" rather than
	// an internal fault.
	RouteCallback(ctx context.Context, callback string, args []string) error
}
