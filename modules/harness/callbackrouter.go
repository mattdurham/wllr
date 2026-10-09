package harness

// NOTE: Any changes to this file must be reflected in the corresponding SPECS.md or NOTES.md.

import (
	"context"
	"encoding/json"

	"github.com/mattdurham/wllr/modules/extension"
	"github.com/mattdurham/wllr/modules/sdk"
)

// ExtensionRouter is the harness-side sdk.UICallbackRouter. It applies the same
// split the interactive Model applies: an extension-owned callback is
// dispatched to the extension host as an EventOnCommand (this is how
// agents:focus, agents:kill, transcript rebuild, and any WASM extension button
// reach their handler), while a core-owned ("__wllr:") callback is handed to an
// injected CoreCallbackHandler.
//
// The TUI resolves callbacks inside its own update loop, so it does not use
// this router. A non-terminal front end has no such loop, so it installs one of
// these and POSTs selections back. This is the return path that pairs with the
// UISink's outbound effects.
//
// A nil host yields ErrNoExtensionHost for extension callbacks; a nil core
// handler yields ErrNoCoreHandler for core callbacks. Neither panics, so serve
// mode stays usable with a partial wiring.
type ExtensionRouter struct {
	host *extension.Host
	core sdk.CoreCallbackHandler
}

// Proof ExtensionRouter satisfies the renderer-neutral router at compile time.
var _ sdk.UICallbackRouter = (*ExtensionRouter)(nil)

// NewExtensionRouter returns a router dispatching extension callbacks to host
// and core callbacks to core. Either may be nil.
func NewExtensionRouter(host *extension.Host, core sdk.CoreCallbackHandler) *ExtensionRouter {
	return &ExtensionRouter{host: host, core: core}
}

// RouteCallback implements sdk.UICallbackRouter.
func (r *ExtensionRouter) RouteCallback(ctx context.Context, callback string, args []string) error {
	if callback == "" {
		return sdk.ErrNoCallbackRouter
	}
	if sdk.IsCoreCallback(callback) {
		if r.core == nil {
			return sdk.ErrNoCoreHandler
		}
		return r.core.HandleCoreCallback(ctx, callback, args)
	}
	if r.host == nil {
		return sdk.ErrNoExtensionHost
	}
	payload, err := json.Marshal(sdk.OnCommandPayload{Name: callback, Args: args})
	if err != nil {
		return err
	}
	_, err = r.host.DispatchEvent(ctx, sdk.Event{Type: sdk.EventOnCommand, Payload: payload})
	return err
}

// SetCoreCallbackHandler installs (or replaces) the core handler. It is used by
// serve mode once the front-end-specific model/thinking wiring is available,
// without rebuilding the router.
func (r *ExtensionRouter) SetCoreCallbackHandler(core sdk.CoreCallbackHandler) {
	r.core = core
}
