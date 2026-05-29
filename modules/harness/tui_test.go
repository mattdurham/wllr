package harness

// tui_test.go tests keyboard interactions and TUI-specific behaviors of the
// harness Model. Tests drive the model directly via callUpdate (same technique
// as model_test.go) — no real terminal or program required.

import (
	"testing"

	tea "charm.land/bubbletea/v2"
	"github.com/mattdurham/wllr/modules/agent"
)

// keyMsg is a convenience helper that returns a tea.KeyPressMsg whose
// String() method returns the given keystroke (e.g. "ctrl+c", "pgup").
func keyMsg(code rune, mod tea.KeyMod) tea.KeyPressMsg {
	return tea.KeyPressMsg{Code: code, Mod: mod}
}

// ---- /help command opens a modal ----

func TestModel_HelpCommand_OpensModal(t *testing.T) {
	m := newTestModel()

	// Dispatch /help command through CommandMsg.
	m, cmd := callUpdate(m, CommandMsg{Name: "help", Args: nil})
	if cmd == nil {
		t.Fatal("expected cmd after CommandMsg{help}")
	}
	msg := cmd()
	m, _ = callUpdate(m, msg)

	if m.modalContent == "" {
		t.Error("expected modal to be open after /help, got empty modalContent")
	}
}

// ---- / triggers autocomplete dropdown ----

func TestModel_SlashInput_ShowsDropdown(t *testing.T) {
	m := newTestModel()
	// Set a window size so layout math works.
	m, _ = callUpdate(m, tea.WindowSizeMsg{Width: 120, Height: 40})

	// Inject a "/" directly into the input value and recompute suggestions.
	m.input.SetValue("/")
	m.updateSuggestions()

	if len(m.suggestions) == 0 {
		t.Error("expected suggestions after '/' input, got none")
	}
}

func TestModel_SlashInputEmpty_NoDropdown(t *testing.T) {
	m := newTestModel()
	m.input.SetValue("")
	m.updateSuggestions()

	if len(m.suggestions) != 0 {
		t.Errorf("expected no suggestions for empty input, got %d", len(m.suggestions))
	}
}

func TestModel_SlashInputHelp_FiltersSuggestions(t *testing.T) {
	m := newTestModel()
	m.input.SetValue("/hel")
	m.updateSuggestions()

	// All returned suggestions should have names starting with "hel".
	for _, s := range m.suggestions {
		if len(s.Name) < 3 || s.Name[:3] != "hel" {
			t.Errorf("suggestion %q does not match prefix %q", s.Name, "hel")
		}
	}
}

// ---- pgup / pgdown scroll ----

func TestModel_PgUp_ScrollsChat(t *testing.T) {
	m := newTestModel()
	m, _ = callUpdate(m, tea.WindowSizeMsg{Width: 120, Height: 40})

	// Add many messages so there is content to scroll.
	for i := 0; i < 30; i++ {
		m.chat.AddUserMessage("line of content to fill the viewport")
	}

	// Scroll to the bottom first.
	m.chat.ScrollDown(1000)
	bottomOffset := m.chat.vp.YOffset()

	// Press pgup.
	m, _ = callUpdate(m, keyMsg(tea.KeyPgUp, 0))

	afterPgUp := m.chat.vp.YOffset()
	if afterPgUp >= bottomOffset && bottomOffset > 0 {
		t.Errorf("pgup did not scroll up: before=%d after=%d", bottomOffset, afterPgUp)
	}
}

func TestModel_PgDown_ScrollsChat(t *testing.T) {
	m := newTestModel()
	m, _ = callUpdate(m, tea.WindowSizeMsg{Width: 120, Height: 40})

	// Add messages to create overflow.
	for i := 0; i < 30; i++ {
		m.chat.AddUserMessage("line of content to fill the viewport")
	}

	// Start at the top.
	m.chat.ScrollUp(1000)
	topOffset := m.chat.vp.YOffset()

	// Press pgdown.
	m, _ = callUpdate(m, keyMsg(tea.KeyPgDown, 0))

	afterPgDown := m.chat.vp.YOffset()
	if afterPgDown <= topOffset {
		// pgdown may be no-op if content fits in viewport — that is also OK.
		// Only fail if we know there was overflow.
		if m.chat.vp.TotalLineCount() > m.chat.height {
			t.Errorf("pgdown did not scroll down: before=%d after=%d", topOffset, afterPgDown)
		}
	}
}

// ---- ctrl+c during streaming sets cancelling status ----

func TestModel_CtrlC_DuringStream_SetsCancellingStatus(t *testing.T) {
	pool := agent.NewPool()
	lm := newMockLM("hello", " ", "world")
	_, _ = pool.Spawn("main", lm, agent.SpawnOpts{})
	m := New(pool, "main", nil)

	// Force streaming state on.
	m.streaming = true

	m, _ = callUpdate(m, keyMsg('c', tea.ModCtrl))

	if m.statusBar.statuses["stream"] != "cancelling…" {
		t.Errorf("expected 'cancelling…', got %q", m.statusBar.statuses["stream"])
	}
}

func TestModel_CtrlC_NotStreaming_Quits(t *testing.T) {
	m := newTestModel()
	m.streaming = false

	_, cmd := callUpdate(m, keyMsg('c', tea.ModCtrl))
	if cmd == nil {
		t.Fatal("expected quit cmd from ctrl+c when not streaming")
	}
	// We can't easily check tea.Quit without running the program, but we
	// can verify a cmd was returned (not nil).
}

// ---- StreamDoneMsg clears working indicator ----

func TestModel_StreamDoneMsg_ClearsWorkingIndicator(t *testing.T) {
	m := newTestModel()
	m.streaming = true
	m.statusBar.statuses["stream"] = "working..."

	m, _ = callUpdate(m, StreamDoneMsg{Err: nil})

	if _, ok := m.statusBar.statuses["stream"]; ok {
		t.Error("stream status should be cleared by StreamDoneMsg")
	}
	if m.streaming {
		t.Error("streaming should be false after StreamDoneMsg")
	}
}

// ---- Modal keyboard navigation ----

func TestModel_Modal_EscClosesIt(t *testing.T) {
	m := newTestModel()
	m.modalContent = "some help text"

	m, _ = callUpdate(m, keyMsg(tea.KeyEscape, 0))
	if m.modalContent != "" {
		t.Errorf("modal should be closed after esc, got %q", m.modalContent)
	}
}

func TestModel_Modal_EnterClosesIt(t *testing.T) {
	m := newTestModel()
	m.modalContent = "some help text"

	m, _ = callUpdate(m, keyMsg(tea.KeyEnter, 0))
	if m.modalContent != "" {
		t.Errorf("modal should be closed after enter, got %q", m.modalContent)
	}
}

// ---- SubmitMsg adds user message to chat ----

func TestModel_SubmitMsg_UserBoxVisibleInChat(t *testing.T) {
	pool := newTestPool()
	m := New(pool, "main", nil)
	m, _ = callUpdate(m, tea.WindowSizeMsg{Width: 120, Height: 40})

	m, _ = callUpdate(m, SubmitMsg{Content: "hello there"})

	// Chat should have exactly one message (the user message).
	if len(m.chat.messages) != 1 {
		t.Fatalf("expected 1 chat message, got %d", len(m.chat.messages))
	}
	if m.chat.messages[0].content != "hello there" {
		t.Errorf("chat message content: got %q, want %q", m.chat.messages[0].content, "hello there")
	}
}

// ---- Dropdown navigation ----

func TestModel_Dropdown_UpDownNavigation(t *testing.T) {
	m := newTestModel()
	m, _ = callUpdate(m, tea.WindowSizeMsg{Width: 120, Height: 40})

	// Open dropdown.
	m.input.SetValue("/")
	m.updateSuggestions()

	if len(m.suggestions) < 2 {
		t.Skip("not enough commands for dropdown nav test")
	}

	initial := m.suggestionIdx
	// Press down.
	m, _ = callUpdate(m, keyMsg(tea.KeyDown, 0))
	if m.suggestionIdx != initial+1 {
		t.Errorf("down: suggestionIdx = %d, want %d", m.suggestionIdx, initial+1)
	}
	// Press up.
	m, _ = callUpdate(m, keyMsg(tea.KeyUp, 0))
	if m.suggestionIdx != initial {
		t.Errorf("up: suggestionIdx = %d, want %d", m.suggestionIdx, initial)
	}
}

func TestModel_Dropdown_EscCloses(t *testing.T) {
	m := newTestModel()
	m.input.SetValue("/")
	m.updateSuggestions()

	if len(m.suggestions) == 0 {
		t.Skip("no suggestions to test")
	}

	m, _ = callUpdate(m, keyMsg(tea.KeyEscape, 0))
	if len(m.suggestions) != 0 {
		t.Errorf("dropdown should be closed after esc, got %d suggestions", len(m.suggestions))
	}
}
