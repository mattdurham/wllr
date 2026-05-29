package testutil

// FakeProvider is a fantasy.Provider that returns a single FakeLM for any model.
type FakeProvider struct {
	lm *FakeLM
}
