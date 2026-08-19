package harness

// NOTE: Any changes to this file must be reflected in the corresponding SPECS.md or NOTES.md.

import (
	"strings"

	"charm.land/bubbles/v2/textinput"
	tea "charm.land/bubbletea/v2"
)

// TextInputView is a fullscreen overlay text input shown instead of the chat.
type TextInputView struct {
	Title    string
	Callback string
	input    textinput.Model
	width    int
	height   int
	active   bool
}

// Open activates the text input overlay with the given title, placeholder,
// and initial value, and focuses the underlying textinput.Model.
func (t *TextInputView) Open(title, placeholder, initialValue, callback string) {
	t.Title = title
	t.Callback = callback
	t.input = textinput.New()
	t.input.Placeholder = placeholder
	t.input.SetValue(initialValue)
	t.input.CursorEnd()
	t.input.Focus()
	t.active = true
}

// Close deactivates the text input overlay.
func (t *TextInputView) Close() {
	t.active = false
	t.Callback = ""
}

// IsActive reports whether the text input overlay is currently shown.
func (t *TextInputView) IsActive() bool { return t.active }

// SetSize updates the dimensions available to the text input overlay.
func (t *TextInputView) SetSize(width, height int) {
	t.width = width
	t.height = height
	innerWidth := width - 4
	if innerWidth < 1 {
		innerWidth = 1
	}
	t.input.SetWidth(innerWidth)
}

// HandleKey handles a key press. Returns:
//   - submitted=true, value=current input text when the user confirms with Enter
//   - cancelled=true when the user presses Esc
//   - otherwise the key is forwarded into the textinput.Model and its Cmd is returned
func (t *TextInputView) HandleKey(kp tea.KeyPressMsg) (submitted bool, value string, cancelled bool, cmd tea.Cmd) {
	switch kp.String() {
	case keyEsc:
		return false, "", true, nil
	case "enter":
		return true, t.input.Value(), false, nil
	}
	t.input, cmd = t.input.Update(kp)
	return false, "", false, cmd
}

// View renders the text input overlay into a string of exactly t.height lines.
func (t *TextInputView) View() string {
	if t.width < 10 {
		t.width = 10
	}
	innerWidth := t.width - 2 // subtract ╭ and ╮
	contentWidth := innerWidth - 2
	if contentWidth < 1 {
		contentWidth = 1
	}

	var sb strings.Builder

	// Top border with title.
	titleStr := " " + t.Title + " "
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

	// Input line.
	line := t.input.View()
	lineRunes := len([]rune(line))
	if lineRunes < contentWidth {
		line += strings.Repeat(" ", contentWidth-lineRunes)
	}
	sb.WriteString(pickerBorderStyle.Render("│") + " " + pickerLabelStyle.Render(line) + " " + pickerBorderStyle.Render("│") + "\n")

	// Fill remaining rows with empty lines.
	visible := t.height - 3
	if visible < 0 {
		visible = 0
	}
	emptyLine := strings.Repeat(" ", contentWidth)
	for rendered := 1; rendered < visible; rendered++ {
		sb.WriteString(pickerBorderStyle.Render("│") + " " + emptyLine + " " + pickerBorderStyle.Render("│") + "\n")
	}

	// Footer hint.
	hint := " enter submit · esc cancel "
	hintRunes := len([]rune(hint))
	botFill := innerWidth - hintRunes
	if botFill < 0 {
		botFill = 0
		hint = hint[:innerWidth]
	}
	sb.WriteString(pickerBorderStyle.Render("╰"+hint+strings.Repeat("─", botFill)+"╯") + "\n")

	return sb.String()
}
