package testutil

// NOTE: Any changes to this file must be reflected in the corresponding SPECS.md or NOTES.md.

import (
	"context"

	"charm.land/fantasy"
)

// FakeProvider is a fantasy.Provider that returns a single FakeLM for any model.

// compile-time interface assertion
var _ fantasy.Provider = (*FakeProvider)(nil)

// NewFakeProvider creates a FakeProvider whose FakeLM will emit the given
// response strings in order (one per call). After responses are exhausted the
// last response is repeated indefinitely.
func NewFakeProvider(responses ...string) *FakeProvider {
	return &FakeProvider{
		lm: &FakeLM{
			responses: responses,
			modelID:   "fake-model",
			provider:  "fake",
		},
	}
}

// LM returns the underlying FakeLM so tests can configure tool call behavior
// or inspect recorded calls.
func (p *FakeProvider) LM() *FakeLM { return p.lm }

// Name implements fantasy.Provider.
func (p *FakeProvider) Name() string { return "fake" }

// LanguageModel implements fantasy.Provider. Returns the same FakeLM for any model ID.
func (p *FakeProvider) LanguageModel(_ context.Context, _ string) (fantasy.LanguageModel, error) {
	return p.lm, nil
}
