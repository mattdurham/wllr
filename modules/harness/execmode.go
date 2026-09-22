package harness

// NOTE: Any changes to this file must be reflected in the corresponding SPECS.md or NOTES.md.

import (
	"fmt"
	"io"

	tea "charm.land/bubbletea/v2"
)

// SubmitExecPrompt puts the model into one-shot mode: it submits prompt as the
// first user turn and quits the program when that turn completes. Used by
// `wllr --exec`, which runs the same program as the TUI with the renderer
// disabled so exec cannot drift from the interactive path.
//
// Quitting on turn completion is what makes the mode non-interactive: exec
// returns control as soon as the agent stops, rather than waiting for input.
func (m *Model) SubmitExecPrompt(prompt string) {
	if prompt == "" {
		return
	}
	m.execMode = true
	// Deferred to Init so the program is running before the turn starts; a
	// Submit issued during construction would race the program's start.
	m.execPrompt = prompt
}

// execQuitOnDone is returned by Init in exec mode to submit the queued prompt
// once bubbletea is running. Bubbletea executes commands on its own goroutine,
// so this cannot race program startup.
func (m *Model) execQuitOnDone() tea.Cmd {
	prompt := m.execPrompt
	if !m.execMode || prompt == "" {
		return nil
	}
	m.execPrompt = ""
	return func() tea.Msg { return SubmitMsg{Content: prompt} }
}

// maybeQuitExec ends the program when a one-shot turn finishes, so `--exec`
// exits on its own. Only the turn started by this mode ends the run: a sub-agent
// finishing must not terminate the process while the main turn is still working.
func (m *Model) maybeQuitExec() tea.Cmd {
	if !m.execMode {
		return nil
	}
	m.execMode = false
	return tea.Quit
}

// SetExecWriter sets the destination for the assistant's final response in exec
// mode. The renderer is disabled there, so without this the response would be
// discarded. Set by cmd before Run.
func (m *Model) SetExecWriter(w io.Writer) { m.execWriter = w }

// writeExecResponse prints the completed response in exec mode. Called from the
// turn-completion path with the same text the transcript would have shown.
func (m *Model) writeExecResponse(text string) {
	if !m.execMode || m.execWriter == nil || text == "" {
		return
	}
	// A write failure here has nowhere useful to surface: the run is already
	// ending and the response is a best-effort convenience over the transcript.
	if _, err := fmt.Fprintln(m.execWriter, text); err != nil {
		return
	}
}
