package harness

import (
	"testing"
)

func TestApplyAuthChoice_RecordsAndClears(t *testing.T) {
	var gotProvider, gotMethod string
	m := &Model{
		authPromptProvider: "anthropic",
		RecordAuthFn: func(provider, method string) error {
			gotProvider, gotMethod = provider, method
			return nil
		},
	}

	m.applyAuthChoice(authMethodOAuth)

	if gotProvider != "anthropic" || gotMethod != authMethodOAuth {
		t.Errorf("RecordAuthFn got (%q,%q), want (anthropic,oauth)", gotProvider, gotMethod)
	}
	if m.authPromptProvider != "" {
		t.Errorf("authPromptProvider should be cleared after applying, got %q", m.authPromptProvider)
	}
}

func TestApplyAuthChoice_NoProvider_NoOp(t *testing.T) {
	called := false
	m := &Model{
		authPromptProvider: "",
		RecordAuthFn:       func(string, string) error { called = true; return nil },
	}
	m.applyAuthChoice(authMethodAPIKey)
	if called {
		t.Error("RecordAuthFn should not be called when no provider is pending")
	}
}

func TestSetPendingAuthProvider_DrivesInitPrompt(t *testing.T) {
	m := New(nil, "main", nil)
	m.SetPendingAuthProvider("openai")
	if m.pendingAuthProvider != "openai" {
		t.Errorf("pendingAuthProvider = %q, want openai", m.pendingAuthProvider)
	}
	// Init should emit a command (the auth prompt) when a provider is pending.
	if cmd := m.Init(); cmd == nil {
		t.Error("Init should return a non-nil batch when an auth prompt is pending")
	}
}
