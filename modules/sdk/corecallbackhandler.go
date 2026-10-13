package sdk

// NOTE: Any changes to this file must be reflected in the corresponding SPECS.md or NOTES.md.

import "context"

// CoreCallbackHandler handles the core-owned callbacks a router receives. It is
// the seam that lets a non-terminal front end perform engine actions (switch
// model, set thinking level) that the TUI normally performs inside its own
// update loop.
type CoreCallbackHandler interface {
	HandleCoreCallback(ctx context.Context, callback string, args []string) error
}
