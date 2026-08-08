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

func TestSetPendingSetupWizard_DrivesInitWizard(t *testing.T) {
	m := New(nil, "main", nil)
	m.SetPendingSetupWizard()
	if !m.pendingSetupWizard {
		t.Error("pendingSetupWizard should be true")
	}
	if cmd := m.Init(); cmd == nil {
		t.Error("Init should return a non-nil batch when setup wizard is pending")
	}
}

func TestSetPendingModelPicker_DrivesInitPrompt(t *testing.T) {
	m := newTestModel()
	m.SetPendingModelPicker()
	if !m.pendingModelPicker {
		t.Fatal("pendingModelPicker should be true")
	}
	if cmd := m.Init(); cmd == nil {
		t.Fatal("Init should include the model picker command")
	}
}

func TestLoginProviderSelected_CloudRecordsOAuthAndBeginsLogin(t *testing.T) {
	var gotProvider, gotMethod, beganProvider string
	m := New(nil, "main", nil)
	m.SelectProviderFn = func(provider string) (string, bool, error) {
		return "gpt-5.5", true, nil
	}
	m.RecordAuthFn = func(provider, method string) error {
		gotProvider, gotMethod = provider, method
		return nil
	}
	m.BeginOAuthFn = func(provider string) (string, string, error) {
		beganProvider = provider
		return "modal", "https://example.test/login", nil
	}
	m.AwaitOAuthFn = func() (string, bool) { return "", false }

	next, cmd := m.Update(loginProviderSelectedMsg{Provider: "openai"})
	m = next.(Model)

	if gotProvider != "openai" || gotMethod != authMethodOAuth {
		t.Errorf("RecordAuthFn got (%q,%q), want (openai,oauth)", gotProvider, gotMethod)
	}
	if beganProvider != "openai" {
		t.Errorf("BeginOAuthFn provider = %q, want openai", beganProvider)
	}
	if m.oauthCaptureProvider != "openai" {
		t.Errorf("oauthCaptureProvider = %q, want openai", m.oauthCaptureProvider)
	}
	if m.activeModel != "gpt-5.5" {
		t.Errorf("activeModel = %q, want gpt-5.5", m.activeModel)
	}
	if m.modalContent != "modal" {
		t.Errorf("modalContent = %q, want modal", m.modalContent)
	}
	if cmd == nil {
		t.Fatal("expected command from beginOAuthLogin")
	}
}

func TestLoginProviderSelected_LocalDoesNotBeginLogin(t *testing.T) {
	recorded := false
	began := false
	m := New(nil, "main", nil)
	m.SelectProviderFn = func(provider string) (string, bool, error) {
		return "llama3.2", false, nil
	}
	m.RecordAuthFn = func(string, string) error { recorded = true; return nil }
	m.BeginOAuthFn = func(string) (string, string, error) {
		began = true
		return "", "", nil
	}
	m.AwaitOAuthFn = func() (string, bool) { return "", false }

	next, cmd := m.Update(loginProviderSelectedMsg{Provider: "local"})
	m = next.(Model)

	if recorded {
		t.Error("local provider should not record OAuth auth choice")
	}
	if began {
		t.Error("local provider should not begin OAuth login")
	}
	if cmd != nil {
		t.Error("local provider selection should not return an OAuth command")
	}
	if m.activeProvider != "local" || m.activeModel != "llama3.2" {
		t.Errorf("selection got provider=%q model=%q, want local/llama3.2", m.activeProvider, m.activeModel)
	}
}
