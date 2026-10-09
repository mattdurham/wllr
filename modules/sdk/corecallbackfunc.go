package sdk

// NOTE: Any changes to this file must be reflected in the corresponding SPECS.md or NOTES.md.

import "context"

// CoreCallbackFunc adapts a function to CoreCallbackHandler.
type CoreCallbackFunc func(ctx context.Context, callback string, args []string) error

// HandleCoreCallback implements CoreCallbackHandler.
func (f CoreCallbackFunc) HandleCoreCallback(ctx context.Context, callback string, args []string) error {
	return f(ctx, callback, args)
}
