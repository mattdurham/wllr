package testutil

// NOTE: Any changes to this file must be reflected in the corresponding SPECS.md or NOTES.md.

// FakeProvider is a fantasy.Provider that returns a single FakeLM for any model.
type FakeProvider struct {
	lm *FakeLM
}
