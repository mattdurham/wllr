package sdk

// NOTE: Any changes to this file must be reflected in the corresponding SPECS.md or NOTES.md.

import "context"

// FuncRouter is a programmatic UICallbackRouter backed by optional function
// fields. The extension field receives extension-owned callbacks; the core
// field receives core-owned ("__wllr:") callbacks. A nil field yields the
// matching sentinel error rather than a panic, so an embedder can implement
// only the direction it needs.
//
// Like FuncSink, it doubles as the test double for code that routes callbacks.
type FuncRouter struct {
	Extension func(ctx context.Context, callback string, args []string) error
	Core      func(ctx context.Context, callback string, args []string) error
}

// Proof FuncRouter satisfies the router contract at compile time.
var _ UICallbackRouter = (*FuncRouter)(nil)

// RouteCallback routes callback to the core or extension handler per
// IsCoreCallback.
func (r *FuncRouter) RouteCallback(ctx context.Context, callback string, args []string) error {
	if IsCoreCallback(callback) {
		if r.Core == nil {
			return ErrNoCoreHandler
		}
		return r.Core(ctx, callback, args)
	}
	if r.Extension == nil {
		return ErrNoExtensionHost
	}
	return r.Extension(ctx, callback, args)
}
