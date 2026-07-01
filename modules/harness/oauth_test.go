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
		BeginOAuthFn: func(provider string) (string, error) {
			return "https://claude.ai/oauth/authorize?x=1", nil
		},
	}
	m.beginOAuthLogin("anthropic")

	if m.oauthCaptureProvider != "anthropic" {
		t.Errorf("oauthCaptureProvider = %q, want anthropic", m.oauthCaptureProvider)
	}
	if m.modalContent == "" {
		t.Error("expected a modal with sign-in instructions")
	}
}

func TestBeginOAuthLogin_ErrorNoCapture(t *testing.T) {
	m := &Model{
		BeginOAuthFn: func(string) (string, error) { return "", errors.New("boom") },
		extHost:      nil,
	}
	m.beginOAuthLogin("anthropic")
	if m.oauthCaptureProvider != "" {
		t.Error("should not enter capture mode when begin fails")
	}
}

func TestCompleteOAuthLogin_CallsCompleteAndClears(t *testing.T) {
	var gotProvider, gotInput string
	m := &Model{
		oauthCaptureProvider: "anthropic",
		CompleteOAuthFn: func(provider, input string) error {
			gotProvider, gotInput = provider, input
			return nil
		},
	}
	cmd := m.completeOAuthLogin("the-code#state")
	if m.oauthCaptureProvider != "" {
		t.Error("capture mode should be cleared immediately")
	}
	msg := drainMsg(cmd)
	n, ok := msg.(NotifyMsg)
	if !ok {
		t.Fatalf("expected NotifyMsg, got %T", msg)
	}
	if gotProvider != "anthropic" || gotInput != "the-code#state" {
		t.Errorf("CompleteOAuthFn got (%q,%q)", gotProvider, gotInput)
	}
	if n.Text == "" {
		t.Error("expected a success notification")
	}
}

func TestCompleteOAuthLogin_ErrorSurfaced(t *testing.T) {
	m := &Model{
		oauthCaptureProvider: "anthropic",
		CompleteOAuthFn:      func(string, string) error { return errors.New("bad code") },
	}
	msg := drainMsg(m.completeOAuthLogin("x"))
	n, ok := msg.(NotifyMsg)
	if !ok {
		t.Fatalf("expected NotifyMsg, got %T", msg)
	}
	if n.Text == "" {
		t.Error("expected an error notification")
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
