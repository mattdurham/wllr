package harness

// NOTE: Any changes to this file must be reflected in the corresponding SPECS.md or NOTES.md.

import (
	"strings"

	tea "charm.land/bubbletea/v2"
	lipgloss "charm.land/lipgloss/v2"
	"github.com/mattdurham/wllr/sdk"
)

var (
	pickerBorderStyle   = lipgloss.NewStyle().Foreground(lipgloss.Color("#89CFF0"))
	pickerSelectedStyle = lipgloss.NewStyle().Background(lipgloss.Color("#1A4A8A")).Foreground(lipgloss.Color("#FFFFFF"))
	pickerLabelStyle    = lipgloss.NewStyle().Foreground(lipgloss.Color("#FFFFFF"))
	pickerSubStyle      = lipgloss.NewStyle().Foreground(lipgloss.Color("#888888"))
	pickerTitleStyle    = lipgloss.NewStyle().Foreground(lipgloss.Color("#89CFF0"))
)

// PickerView is a fullscreen overlay list picker shown instead of the chat.
type PickerView struct {
	Title    string
	Items    []sdk.ShowPickerItem
	Callback string

	selectedIdx  int
	scrollOffset int
	width        int
	height       int
	active       bool
}

// Open activates the picker with the given items and resets navigation.
func (p *PickerView) Open(title string, items []sdk.ShowPickerItem, callback string) {
	p.Title = title
	p.Items = items
	p.Callback = callback
	p.selectedIdx = 0
	p.scrollOffset = 0
	p.active = true
}

// Close deactivates the picker.
func (p *PickerView) Close() {
	p.active = false
	p.Items = nil
	p.Callback = ""
}

// IsActive reports whether the picker overlay is currently shown.
func (p *PickerView) IsActive() bool { return p.active }

// SetSize updates the dimensions available to the picker.
func (p *PickerView) SetSize(width, height int) {
	p.width = width
	p.height = height
}

// HandleKey handles a key press. Returns:
//   - selected=true, id=chosen item ID when the user confirms
//   - cancelled=true when the user presses Esc
//   - otherwise (false, "", false) meaning key was consumed but no action yet
func (p *PickerView) HandleKey(kp tea.KeyPressMsg) (selected bool, id string, cancelled bool) {
	switch kp.String() {
	case "esc":
		return false, "", true
	case "enter":
		if len(p.Items) == 0 {
			return false, "", true
		}
		return true, p.Items[p.selectedIdx].ID, false
	case "up":
		if p.selectedIdx > 0 {
			p.selectedIdx--
			if p.selectedIdx < p.scrollOffset {
				p.scrollOffset = p.selectedIdx
			}
		}
	case "down":
		if p.selectedIdx < len(p.Items)-1 {
			p.selectedIdx++
			visible := p.visibleRows()
			if p.selectedIdx >= p.scrollOffset+visible {
				p.scrollOffset = p.selectedIdx - visible + 1
			}
		}
	}
	return false, "", false
}

// visibleRows returns how many items fit in the content area.
func (p *PickerView) visibleRows() int {
	// Top border(1) + bottom border(1) + footer hint(1) = 3 overhead lines.
	rows := p.height - 3
	if rows < 1 {
		rows = 1
	}
	return rows
}

// View renders the picker into a string of exactly p.height lines.
func (p *PickerView) View() string {
	if p.width < 10 {
		p.width = 10
	}
	innerWidth := p.width - 2 // subtract ╭ and ╮
	contentWidth := innerWidth - 2
	if contentWidth < 1 {
		contentWidth = 1
	}

	var sb strings.Builder

	// Top border with title.
	titleStr := " " + p.Title + " "
	titleRunes := len([]rune(titleStr))
	fillLen := innerWidth - titleRunes
	if fillLen < 0 {
		fillLen = 0
	}
	topFill := strings.Repeat("─", fillLen)
	sb.WriteString(pickerBorderStyle.Render("╭") + pickerTitleStyle.Render(titleStr) + pickerBorderStyle.Render(topFill+"╮") + "\n")

	visible := p.visibleRows()
	end := p.scrollOffset + visible
	if end > len(p.Items) {
		end = len(p.Items)
	}

	rendered := 0
	for i := p.scrollOffset; i < end; i++ {
		item := p.Items[i]
		selected := i == p.selectedIdx

		label := item.Label
		sub := item.Sublabel

		// Build the line content: label  sublabel (truncated to fit).
		var line string
		if sub != "" {
			gap := 2
			maxLabel := contentWidth - gap - 1
			if maxLabel < 1 {
				maxLabel = 1
			}
			lr := []rune(label)
			if len(lr) > maxLabel {
				lr = lr[:maxLabel]
			}
			sr := []rune(sub)
			remaining := contentWidth - len(lr) - gap
			if remaining < 0 {
				remaining = 0
			}
			if len(sr) > remaining {
				sr = sr[:remaining]
			}
			line = string(lr) + strings.Repeat(" ", gap) + string(sr)
		} else {
			lr := []rune(label)
			if len(lr) > contentWidth {
				lr = lr[:contentWidth]
			}
			line = string(lr)
		}

		// Pad to contentWidth.
		lr := []rune(line)
		if len(lr) < contentWidth {
			line = line + strings.Repeat(" ", contentWidth-len(lr))
		}

		if selected {
			sb.WriteString(pickerBorderStyle.Render("│") + " " + pickerSelectedStyle.Render(line) + " " + pickerBorderStyle.Render("│") + "\n")
		} else {
			sb.WriteString(pickerBorderStyle.Render("│") + " " + pickerLabelStyle.Render(line) + " " + pickerBorderStyle.Render("│") + "\n")
		}
		rendered++
	}

	// Fill remaining rows with empty lines.
	emptyLine := strings.Repeat(" ", contentWidth)
	for rendered < visible {
		sb.WriteString(pickerBorderStyle.Render("│") + " " + emptyLine + " " + pickerBorderStyle.Render("│") + "\n")
		rendered++
	}

	// Footer hint.
	hint := " ↑↓ navigate · enter select · esc cancel "
	hintRunes := len([]rune(hint))
	botFill := innerWidth - hintRunes
	if botFill < 0 {
		botFill = 0
		hint = hint[:innerWidth]
	}
	sb.WriteString(pickerBorderStyle.Render("╰"+hint+strings.Repeat("─", botFill)+"╯") + "\n")

	return sb.String()
}
