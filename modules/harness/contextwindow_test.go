package harness

import "testing"

func TestModel_UnknownModelPromptsForContextWindow(t *testing.T) {
	m := New(nil, "main", nil)
	m.activeProvider = "openai"
	m.SelectModelFn = func(string) error { return ErrContextWindowRequired }
	m.applyModelSelection("vendor-model")
	if !m.textInput.IsActive() {
		t.Fatal("expected context-window prompt to open")
	}
	if m.textInput.Callback != contextWindowCallback {
		t.Fatalf("callback = %q, want %q", m.textInput.Callback, contextWindowCallback)
	}
}

func TestParseContextWindow_RequiresPositiveInteger(t *testing.T) {
	for _, value := range []string{"", "0", "-1", "not-a-number"} {
		if _, err := parseContextWindow(value); err == nil {
			t.Errorf("parseContextWindow(%q) succeeded, want error", value)
		}
	}
	if got, err := parseContextWindow(" 128000 "); err != nil || got != 128000 {
		t.Fatalf("parseContextWindow = (%d, %v), want (128000, nil)", got, err)
	}
}
