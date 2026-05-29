package testutil

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
