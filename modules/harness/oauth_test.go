package harness

import (
	"errors"
	"testing"

	tea "charm.land/bubbletea/v2"
)

func drainMsg(cmd tea.Cmd) tea.Msg {
	if cmd == nil {
		return nil
	}
	return cmd()
}

func TestBeginOAuthLogin_EntersCaptureMode(t *testing.T) {
	m := &Model{
		BeginOAuthFn: func(provider string) (string, string, error) {
			return "Sign in to anthropic\n\nhttps://claude.ai/oauth/authorize?x=1", "https://claude.ai/oauth/authorize?x=1", nil
		},
		AwaitOAuthFn: func() (string, bool) { return "", false },
	}
	cmd := m.beginOAuthLogin("anthropic")

	if m.oauthCaptureProvider != "anthropic" {
		t.Errorf("oauthCaptureProvider = %q, want anthropic", m.oauthCaptureProvider)
	}
	if m.modalContent == "" {
		t.Error("expected a modal with sign-in instructions")
	}
	// begin returns a batch (await + clipboard copy).
	if cmd == nil {
		t.Error("expected a non-nil command from beginOAuthLogin")
	}
}

func TestBeginOAuthLogin_ErrorNoCapture(t *testing.T) {
	m := &Model{
		BeginOAuthFn: func(string) (string, string, error) { return "", "", errors.New("boom") },
		AwaitOAuthFn: func() (string, bool) { return "", false },
	}
	m.beginOAuthLogin("anthropic")
	if m.oauthCaptureProvider != "" {
		t.Error("should not enter capture mode when begin fails")
	}
}

func TestBeginOAuthLogin_UnavailableWithoutCallback(t *testing.T) {
	// No AwaitOAuthFn ⇒ login is unavailable.
	m := &Model{
		BeginOAuthFn: func(string) (string, string, error) { return "body", "https://x", nil },
	}
	m.beginOAuthLogin("anthropic")
	if m.oauthCaptureProvider != "" {
		t.Error("should not enter capture mode without a callback awaiter")
	}
}

func TestCompleteOAuthFromCallback_CompletesWhenCapturing(t *testing.T) {
	var gotProvider, gotInput string
	m := &Model{
		oauthCaptureProvider: "anthropic",
		CompleteOAuthFn: func(provider, input string) error {
			gotProvider, gotInput = provider, input
			return nil
		},
	}
	cmd := m.completeOAuthFromCallback(oauthCallbackMsg{Input: "code=abc&state=xyz", OK: true})
	if cmd == nil {
		t.Fatal("expected a completion command")
	}
	if m.oauthCaptureProvider != "" {
		t.Error("capture mode should be cleared")
	}
	if _, ok := drainMsg(cmd).(NotifyMsg); !ok {
		t.Error("expected a NotifyMsg")
	}
	if gotProvider != "anthropic" || gotInput != "code=abc&state=xyz" {
		t.Errorf("CompleteOAuthFn got (%q,%q)", gotProvider, gotInput)
	}
}

func TestCompleteOAuthFromCallback_ErrorSurfaced(t *testing.T) {
	m := &Model{
		oauthCaptureProvider: "anthropic",
		CompleteOAuthFn:      func(string, string) error { return errors.New("bad code") },
	}
	msg := drainMsg(m.completeOAuthFromCallback(oauthCallbackMsg{Input: "x", OK: true}))
	n, ok := msg.(NotifyMsg)
	if !ok {
		t.Fatalf("expected NotifyMsg, got %T", msg)
	}
	if n.Text == "" {
		t.Error("expected an error notification")
	}
}

func TestCompleteOAuthFromCallback_IgnoredWhenNotCapturing(t *testing.T) {
	called := false
	m := &Model{
		oauthCaptureProvider: "", // login already finished (e.g. via paste)
		CompleteOAuthFn:      func(string, string) error { called = true; return nil },
	}
	if cmd := m.completeOAuthFromCallback(oauthCallbackMsg{Input: "x", OK: true}); cmd != nil {
		t.Error("expected nil command when not capturing")
	}
	// ok=false must also be a no-op.
	m.oauthCaptureProvider = "anthropic"
	if cmd := m.completeOAuthFromCallback(oauthCallbackMsg{OK: false}); cmd != nil {
		t.Error("expected nil command when ok=false")
	}
	if called {
		t.Error("CompleteOAuthFn should not be called")
	}
}

func TestBuiltinLogin_EmitsLoginMsg(t *testing.T) {
	r := NewRegistry()
	registerBuiltins(r)
	cmd := r.Dispatch("login", nil)
	if cmd == nil {
		t.Fatal("expected non-nil Cmd")
	}
	if _, ok := cmd().(loginMsg); !ok {
		t.Errorf("expected loginMsg, got %T", cmd())
	}
}
