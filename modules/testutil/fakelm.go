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

	// script holds queued scripted turns. When non-empty, Stream pops the
	// first entry and emits it instead of the preset text responses.
	script []ScriptedTurn
}

// NewFakeLM creates a FakeLM with no preset responses and an empty script.
// Use SetScript to configure scripted turns, or NewFakeLMWithResponses for text-only turns.
func NewFakeLM() *FakeLM {
	return &FakeLM{
		modelID:  "fake-model",
		provider: "fake",
	}
}

// NewFakeLMWithResponses creates a FakeLM with the given preset text responses.
// Scripted turns take priority over preset responses when both are configured.
func NewFakeLMWithResponses(responses ...string) *FakeLM {
	return &FakeLM{
		modelID:   "fake-model",
		provider:  "fake",
		responses: responses,
	}
}

// SetScript replaces the scripted turn queue.
// Turns are popped in order by each subsequent call to Stream.
// Once all scripted turns are consumed, Stream falls back to preset text responses.
func (lm *FakeLM) SetScript(turns []ScriptedTurn) {
	lm.mu.Lock()
	lm.script = make([]ScriptedTurn, len(turns))
	copy(lm.script, turns)
	lm.mu.Unlock()
}
