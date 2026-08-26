package harness

// NOTE: Any changes to this file must be reflected in the corresponding SPECS.md or NOTES.md.

import (
	"errors"
	"fmt"
	"strconv"
	"strings"
)

// ErrContextWindowRequired tells the harness that model selection needs an
// explicit context-window entry before it can continue.
var ErrContextWindowRequired = errors.New("context window required")

const contextWindowCallback = "__wllr:model_context_window"

type contextWindowEnteredMsg struct {
	Provider string
	Model    string
	Value    string
}

func parseContextWindow(value string) (int64, error) {
	n, err := strconv.ParseInt(strings.TrimSpace(value), 10, 64)
	if err != nil || n <= 0 {
		return 0, fmt.Errorf("context window must be a positive number of tokens")
	}
	return n, nil
}

func (m *Model) openContextWindowPrompt(provider, model string) {
	m.pendingContextProvider = provider
	m.pendingContextModel = model
	m.textInput.Open(
		fmt.Sprintf("Context window for %s in tokens  (enter · esc)", model),
		"200000",
		"",
		contextWindowCallback,
	)
	m.textInput.SetSize(m.width, m.chatHeight())
}
