package extension

// NOTE: Any changes to this file must be reflected in the corresponding SPECS.md or NOTES.md.

import (
	"context"

	"github.com/mattdurham/wllr/modules/sdk"
)

// Handler is a function that receives an event from the bus.
// Returning an error is optional — the bus logs it and continues.
type Handler func(ctx context.Context, evt sdk.Event) error
