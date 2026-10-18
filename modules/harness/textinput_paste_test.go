package harness

import (
	"strings"
	"testing"

	tea "charm.land/bubbletea/v2"
)

func TestTextInputPasteStaysInOverlay(t *testing.T) {
	for _, secret := range []bool{false, true} {
		name := "plain"
		if secret {
			name = "secret"
		}
		t.Run(name, func(t *testing.T) {
			m := newTestModel()
			m.input.SetValue("unsent chat")
			if secret {
				m.textInput.OpenSecret("API key", "test")
			} else {
				m.textInput.Open("Endpoint", "", "", "test")
			}
			m.textInput.SetSize(80, 20)
			m, _ = callUpdate(m, tea.PasteMsg{Content: "test-pasted-credential"})
			if got := m.textInput.input.Value(); got != "test-pasted-credential" {
				t.Fatalf("overlay value = %q", got)
			}
			if got := m.input.Value(); got != "unsent chat" {
				t.Fatalf("chat changed to %q", got)
			}
			if secret && strings.Contains(m.textInput.View(), "test-pasted-credential") {
				t.Fatal("secret paste rendered unmasked")
			}
			submitted, value, _, _ := m.textInput.HandleKey(tea.KeyPressMsg{Code: tea.KeyEnter})
			if !submitted || value != "test-pasted-credential" {
				t.Fatal("paste was not available for submission")
			}
		})
	}
}

func TestPasteRespectsOtherOverlays(t *testing.T) {
	for _, overlay := range []string{"chat", "picker", "modal"} {
		t.Run(overlay, func(t *testing.T) {
			m := newTestModel()
			switch overlay {
			case "picker":
				m.picker.Open("Provider", nil, "test")
			case "modal":
				m.modalContent = "Login instructions"
			}
			m, _ = callUpdate(m, tea.PasteMsg{Content: "pasted text"})
			want := ""
			if overlay == "chat" {
				want = "pasted text"
			}
			if got := m.input.Value(); got != want {
				t.Fatalf("chat value = %q, want %q", got, want)
			}
		})
	}
}
