package agent_test

import (
	"context"
	"strings"
	"sync"
	"testing"
	"time"

	"charm.land/fantasy"
	"github.com/mattdurham/wllr/modules/agent"
	"github.com/mattdurham/wllr/modules/sdk"
)

// recordingProvider is a fantasy.Provider that records the last model ID it was
// asked for and returns a trivial streaming LM. Used to assert model rerouting.
type recordingProvider struct {
	mu    sync.Mutex
	model string
}

func (p *recordingProvider) Name() string { return "recording" }

func (p *recordingProvider) LanguageModel(_ context.Context, modelID string) (fantasy.LanguageModel, error) {
	p.mu.Lock()
	p.model = modelID
	p.mu.Unlock()
	return &tokenStreamLM{tokens: []string{"ok"}}, nil
}

func (p *recordingProvider) lastModel() string {
	p.mu.Lock()
	defer p.mu.Unlock()
	return p.model
}

// Interception effects are observed via: (1) block → turn errors with
// ProviderRequestBlockedError; (2) reroute → the provider is asked for the new
// model; (3) redact → the turn completes and history keeps the original
// (redaction is send-time only).

func TestProviderIntercept_BlockFailsTurn(t *testing.T) {
	pool := agent.NewPool()
	pool.SetProviderRequestInterceptor(func(_ string, msgs []sdk.Message, model string) ([]sdk.Message, string, bool, string) {
		return msgs, model, true, "contains api key"
	})
	a, err := pool.Spawn("main", &tokenStreamLM{tokens: []string{"resp"}}, agent.SpawnOpts{})
	if err != nil {
		t.Fatalf("Spawn: %v", err)
	}
	done := make(chan error, 1)
	a.SetOnDone(func(e error) { done <- e })

	a.Submit(testCtx(t), "my key is sk-123")
	gotErr := subWaitDone(t, done, "main")
	if gotErr == nil {
		t.Fatal("expected blocked turn to error, got nil")
	}
	var blockedErr *agent.ProviderRequestBlockedError
	if !asProviderBlocked(gotErr, &blockedErr) {
		t.Fatalf("expected *ProviderRequestBlockedError, got %T: %v", gotErr, gotErr)
	}
	if !strings.Contains(blockedErr.Reason, "api key") {
		t.Errorf("reason: got %q", blockedErr.Reason)
	}
}

func TestProviderIntercept_NoInterceptorTurnSucceeds(t *testing.T) {
	pool := agent.NewPool()
	a, err := pool.Spawn("main", &tokenStreamLM{tokens: []string{"ok"}}, agent.SpawnOpts{})
	if err != nil {
		t.Fatalf("Spawn: %v", err)
	}
	got := collectResponse(t, a, "hello")
	if !strings.Contains(got, "ok") {
		t.Errorf("response: got %q", got)
	}
}

func TestProviderIntercept_RedactPreservesHistoryOriginal(t *testing.T) {
	pool := agent.NewPool()
	// Redact: replace any message content with "[redacted]" on the way out.
	pool.SetProviderRequestInterceptor(func(_ string, msgs []sdk.Message, model string) ([]sdk.Message, string, bool, string) {
		out := make([]sdk.Message, len(msgs))
		for i, m := range msgs {
			out[i] = sdk.Message{Role: m.Role, Content: "[redacted]"}
		}
		return out, model, false, ""
	})
	a, err := pool.Spawn("main", &tokenStreamLM{tokens: []string{"ack"}}, agent.SpawnOpts{})
	if err != nil {
		t.Fatalf("Spawn: %v", err)
	}
	done := make(chan error, 1)
	a.SetOnDone(func(e error) { done <- e })
	a.Submit(testCtx(t), "secret content")
	if e := subWaitDone(t, done, "main"); e != nil {
		t.Fatalf("turn error: %v", e)
	}
	// History must record the ORIGINAL user content, not the redacted form —
	// redaction is send-time only.
	var foundOriginal bool
	for _, m := range a.History() {
		if m.Role == sdk.RoleUser && strings.Contains(m.Content, "secret content") {
			foundOriginal = true
		}
		if strings.Contains(m.Content, "[redacted]") {
			t.Errorf("redacted text leaked into history: %+v", m)
		}
	}
	if !foundOriginal {
		t.Error("original user content missing from history")
	}
}

func TestProviderIntercept_RerouteRequestsNewModel(t *testing.T) {
	prov := &recordingProvider{}
	pool := agent.NewPool()
	pool.SetProvider(prov)
	pool.SetDefaultModelName("frontier")
	pool.SetProviderRequestInterceptor(func(_ string, msgs []sdk.Message, _ string) ([]sdk.Message, string, bool, string) {
		return msgs, "local-cheap", false, "" // reroute
	})
	a, err := pool.Spawn("main", &tokenStreamLM{tokens: []string{"ok"}}, agent.SpawnOpts{ModelName: "frontier"})
	if err != nil {
		t.Fatalf("Spawn: %v", err)
	}
	done := make(chan error, 1)
	a.SetOnDone(func(e error) { done <- e })
	a.Submit(testCtx(t), "route me")
	if e := subWaitDone(t, done, "main"); e != nil {
		t.Fatalf("turn error: %v", e)
	}
	// The reroute must have asked the provider for the new model.
	if got := prov.lastModel(); got != "local-cheap" {
		t.Errorf("provider asked for model %q, want %q", got, "local-cheap")
	}
}

func asProviderBlocked(err error, target **agent.ProviderRequestBlockedError) bool {
	if b, ok := err.(*agent.ProviderRequestBlockedError); ok {
		*target = b
		return true
	}
	return false
}

// give the goroutine a beat in case of scheduling skew (kept small).
var _ = time.Millisecond
