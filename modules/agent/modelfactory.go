package agent

// NOTE: Any changes to this file must be reflected in the corresponding SPECS.md or NOTES.md.

import (
	"context"

	"charm.land/fantasy"
)

// ModelFactory creates a language model for a model name and optional endpoint.
// The provider is the pool's current provider; endpoint is empty when the
// configured endpoint should be selected by the factory.
type ModelFactory func(ctx context.Context, provider fantasy.Provider, model, endpoint string) (fantasy.LanguageModel, error)
