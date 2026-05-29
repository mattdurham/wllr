package testutil

// NOTE: Any changes to this file must be reflected in the corresponding SPECS.md or NOTES.md.

import "sync"

// FakeLM is a fantasy.LanguageModel that streams preset text responses
// one word at a time and records every call it receives.
type FakeLM struct {
	modelID   string
	provider  string
	responses []string
	calls     []RecordedCall
	callIdx   int
	mu        sync.Mutex
}
