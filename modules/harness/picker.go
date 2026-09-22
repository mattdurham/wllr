package harness

// NOTE: Any changes to this file must be reflected in the corresponding SPECS.md or NOTES.md.

import (
	"strings"

	tea "charm.land/bubbletea/v2"
	lipgloss "charm.land/lipgloss/v2"
	"github.com/mattdurham/wllr/modules/sdk"
)

var (
	pickerBorderStyle   = lipgloss.NewStyle().Foreground(lipgloss.Color("#89CFF0"))
	pickerSelectedStyle = lipgloss.NewStyle().
				Background(lipgloss.Color("#1A4A8A")).
				Foreground(lipgloss.Color("#FFFFFF"))
	pickerLabelStyle = lipgloss.NewStyle().Foreground(lipgloss.Color("#FFFFFF"))
	pickerTitleStyle = lipgloss.NewStyle().Foreground(lipgloss.Color("#89CFF0"))
)

// PickerView is a fullscreen overlay list picker shown instead of the chat.

// Open activates the picker with the given items and resets navigation.
func (p *PickerView) Open(title string, items []sdk.ShowPickerItem, callback string) {
	p.Title = title
	p.Items = items
	p.Callback = callback
	p.selectedIdx = 0
	p.scrollOffset = 0
	p.searchable = false
	p.query = ""
	p.filtered = nil
	p.active = true
}

// OpenSearch opens a picker whose items can be narrowed by typing.
func (p *PickerView) OpenSearch(title string, items []sdk.ShowPickerItem, callback string) {
	p.Open(title, items, callback)
	p.searchable = true
	p.filterItems()
}

// Close deactivates the picker.
func (p *PickerView) Close() {
	p.active = false
	p.Items = nil
	p.Callback = ""
	p.query = ""
	p.filtered = nil
	p.searchable = false
}

// IsActive reports whether the picker overlay is currently shown.
func (p *PickerView) IsActive() bool { return p.active }

// Select highlights the item with the given ID, if present, and returns
// whether it was found. Navigation is clamped so the highlight stays visible.
// Used to preserve the cursor across a reopen (e.g. the model picker reopening
// after a tier tag) so consecutive edits apply to the same row.
func (p *PickerView) Select(id string) bool {
	if id == "" {
		return false
	}
	count := len(p.Items)
	if p.searchable {
		count = len(p.filtered)
	}
	for i := 0; i < count; i++ {
		idx := i
		if p.searchable {
			idx = p.filtered[i]
		}
		if p.Items[idx].ID != id {
			continue
		}
		p.selectedIdx = i
		visible := p.visibleRows()
		if p.selectedIdx < p.scrollOffset {
			p.scrollOffset = p.selectedIdx
		}
		if p.selectedIdx >= p.scrollOffset+visible {
			p.scrollOffset = p.selectedIdx - visible + 1
		}
		return true
	}
	return false
}

// Highlighted returns the ID of the currently highlighted item and whether one
// exists. Searchable pickers resolve through the filtered index set.
func (p *PickerView) Highlighted() (string, bool) {
	count := len(p.Items)
	if p.searchable {
		count = len(p.filtered)
	}
	if count == 0 || p.selectedIdx < 0 || p.selectedIdx >= count {
		return "", false
	}
	idx := p.selectedIdx
	if p.searchable {
		idx = p.filtered[idx]
	}
	if idx < 0 || idx >= len(p.Items) {
		return "", false
	}
	return p.Items[idx].ID, true
}

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
	count := len(p.Items)
	if p.searchable {
		count = len(p.filtered)
	}
	switch kp.String() {
	case keyEsc:
		return false, "", true
	case "enter":
		if count == 0 {
			return false, "", false
		}
		idx := p.selectedIdx
		if p.searchable {
			idx = p.filtered[idx]
		}
		return true, p.Items[idx].ID, false
	case "up":
		if p.selectedIdx > 0 {
			p.selectedIdx--
			if p.selectedIdx < p.scrollOffset {
				p.scrollOffset = p.selectedIdx
			}
		}
	case "down":
		if p.selectedIdx < count-1 {
			p.selectedIdx++
			visible := p.visibleRows()
			if p.selectedIdx >= p.scrollOffset+visible {
				p.scrollOffset = p.selectedIdx - visible + 1
			}
		}
	case "backspace":
		if p.searchable && p.query != "" {
			p.query = string([]rune(p.query)[:len([]rune(p.query))-1])
			p.filterItems()
		}
	case "ctrl+u":
		if p.searchable {
			p.query = ""
			p.filterItems()
		}
	default:
		if p.searchable && kp.Text != "" {
			p.query += kp.Text
			p.filterItems()
		}
	}
	return false, "", false
}

func (p *PickerView) filterItems() {
	p.filtered = p.filtered[:0]
	query := strings.ToLower(strings.TrimSpace(p.query))
	for i, item := range p.Items {
		if query == "" || strings.Contains(strings.ToLower(item.Label+" "+item.ID+" "+item.Sublabel), query) {
			p.filtered = append(p.filtered, i)
		}
	}
	p.selectedIdx = 0
	p.scrollOffset = 0
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
	sb.WriteString(
		pickerBorderStyle.Render(
			"╭",
		) + pickerTitleStyle.Render(
			titleStr,
		) + pickerBorderStyle.Render(
			topFill+"╮",
		) + "\n",
	)

	visible := p.visibleRows()
	count := len(p.Items)
	if p.searchable {
		count = len(p.filtered)
	}
	end := p.scrollOffset + visible
	if end > count {
		end = count
	}

	rendered := 0
	for i := p.scrollOffset; i < end; i++ {
		idx := i
		if p.searchable {
			idx = p.filtered[i]
		}
		item := p.Items[idx]
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
			line += strings.Repeat(" ", contentWidth-len(lr))
		}

		if selected {
			sb.WriteString(
				pickerBorderStyle.Render(
					"│",
				) + " " + pickerSelectedStyle.Render(
					line,
				) + " " + pickerBorderStyle.Render(
					"│",
				) + "\n",
			)
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
	if p.searchable {
		hint = " search: " + p.query + " · ↑↓ · enter · esc "
	}
	hintRunes := len([]rune(hint))
	botFill := innerWidth - hintRunes
	if botFill < 0 {
		botFill = 0
		hint = hint[:innerWidth]
	}
	sb.WriteString(pickerBorderStyle.Render("╰"+hint+strings.Repeat("─", botFill)+"╯") + "\n")

	return sb.String()
}
