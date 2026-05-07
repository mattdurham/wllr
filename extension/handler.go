package extension

import (
	"context"

	"github.com/mattdurham/wllr/sdk"
)

// Handler is a function that receives an event from the bus.
// Returning an error is optional — the bus logs it and continues.
type Handler func(ctx context.Context, evt sdk.Event) error
