package harness

// NOTE: Any changes to this file must be reflected in the corresponding SPECS.md or NOTES.md.

import (
	"fmt"
	"sort"
	"strings"
	"time"

	tea "charm.land/bubbletea/v2"
	lipgloss "charm.land/lipgloss/v2"
	"github.com/mattdurham/wllr/sdk"
)

// StatusBar renders provider/model info and keyed status values.
// When overrideLine is non-empty it is used verbatim instead of the
// auto-generated line; extensions and the /status command use this to
// take full control of the status display.
type StatusBar struct {
	statuses     map[string]string
	providerName string
	modelName    string

	// overrideLine, when non-empty, replaces the entire auto-generated line.
	// Set via StatusUpdateMsg{Key: "_override", Value: "..."}.
	// Cleared by StatusUpdateMsg{Key: "_override", Value: ""}.
	overrideLine string
	totalTokens  int
	width        int
}

var statusBarStyle = lipgloss.NewStyle().
	Bold(true).
	Foreground(lipgloss.Color("#FFFFFF")).
	Background(lipgloss.Color("#333333"))

// NewStatusBar creates a StatusBar.
func NewStatusBar(providerName, modelName string) StatusBar {
	return StatusBar{
		providerName: providerName,
		modelName:    modelName,
		statuses:     make(map[string]string),
	}
}

// SetWidth sets the display width for truncation.
func (s *StatusBar) SetWidth(w int) { s.width = w }

// Update handles StatusUpdateMsg.
// The special key "_override" sets or clears the full-line override;
// all other keys update the named status slot as before.
func (s StatusBar) Update(msg tea.Msg) (StatusBar, tea.Cmd) {
	switch m := msg.(type) {
	case StatusUpdateMsg:
		if s.statuses == nil {
			s.statuses = make(map[string]string)
		}
		if m.Key == "_override" {
			s.overrideLine = m.Value
		} else {
			if m.Value == "" {
				delete(s.statuses, m.Key)
			} else {
				s.statuses[m.Key] = m.Value
			}
		}
	}
	return s, nil
}

// AddTokens increments the total token counter.
func (s *StatusBar) AddTokens(n int) { s.totalTokens += n }

// Line returns the bare status text (no lipgloss styling) used in the
// top border of the input box. When an override is active it is returned
// verbatim; otherwise the default provider/model/tokens/statuses line is built.
func (s StatusBar) Line() string {
	if s.overrideLine != "" {
		return s.overrideLine
	}
	return s.defaultLine()
}

// defaultLine builds the auto-generated provider/model/tokens/statuses line.
func (s StatusBar) defaultLine() string {
	var parts []string
	if s.providerName != "" {
		parts = append(parts, s.providerName)
	}
	if s.modelName != "" {
		parts = append(parts, s.modelName)
	}
	if s.totalTokens > 0 {
		parts = append(parts, fmt.Sprintf("tokens:%d", s.totalTokens))
	}
	keys := make([]string, 0, len(s.statuses))
	for k := range s.statuses {
		keys = append(keys, k)
	}
	sort.Strings(keys)
	for _, k := range keys {
		parts = append(parts, fmt.Sprintf("%s:%s", k, s.statuses[k]))
	}
	return strings.Join(parts, "  ")
}

// StatusInfo returns a read-only snapshot of the current status bar state
// for consumption by the get_status_info host call.
// The "_override" key is excluded from Statuses.
func (s StatusBar) StatusInfo(working bool) sdk.StatusInfo {
	statuses := make(map[string]string, len(s.statuses))
	for k, v := range s.statuses {
		statuses[k] = v
	}
	return sdk.StatusInfo{
		Tokens:   s.totalTokens,
		Working:  working,
		Provider: s.providerName,
		Model:    s.modelName,
		Statuses: statuses,
	}
}

// View renders the status bar as a single line (used standalone; the main
// TUI embeds Line() inside the input-box top border instead).
func (s StatusBar) View() string {
	line := s.Line()
	if s.width > 0 && len([]rune(line)) > s.width {
		runes := []rune(line)
		line = string(runes[:s.width-1]) + "…"
	}
	return statusBarStyle.Width(s.width).Render(line)
}

// formatElapsed formats a duration as "42s" or "1m 10s".
func formatElapsed(d time.Duration) string {
	s := int(d.Seconds())
	if s < 60 {
		return fmt.Sprintf("%ds", s)
	}
	return fmt.Sprintf("%dm %ds", s/60, s%60)
}
