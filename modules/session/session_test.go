package session_test

import (
	"context"
	"testing"

	"github.com/mattdurham/wllr/modules/agent"
	"github.com/mattdurham/wllr/modules/extension"
	"github.com/mattdurham/wllr/modules/sdk"
	"github.com/mattdurham/wllr/modules/session"
	"github.com/mattdurham/wllr/modules/testutil"
)

// fakeRenderer satisfies harness.Renderer for tests.
type fakeRenderer struct {
	notifications []string
	tokens        []string
}

func (f *fakeRenderer) AppendToken(t string)       { f.tokens = append(f.tokens, t) }
func (f *fakeRenderer) FinalizeMessage()           {}
func (f *fakeRenderer) AddUserMessage(_, _ string) {}

func (f *fakeRenderer) AddNotification(
	t string,
) {
	f.notifications = append(f.notifications, t)
}
func (f *fakeRenderer) SetStreaming(_ bool, _ error)                          {}
func (f *fakeRenderer) ShowModal(_ string)                                    {}
func (f *fakeRenderer) ShowPicker(_ string, _ []sdk.ShowPickerItem, _ string) {}
func (f *fakeRenderer) AddToolCall(_, _, _, _ string)                         {}
func (f *fakeRenderer) UpdateToolCall(_, _, _ string, _ bool, _ string)       {}
func (f *fakeRenderer) SetStatus(_, _ string)                                 {}
func (f *fakeRenderer) AppendConsoleLine(_ string)                            {}
func (f *fakeRenderer) ClearConsole()                                         {}
func (f *fakeRenderer) Abort()                                                {}
func (f *fakeRenderer) ResetHistory(_ []sdk.Message) error                    { return nil }

func TestWire_ReturnsSession(t *testing.T) {
	ctx := context.Background()
	h := extension.NewHost(nil)
	defer h.Close(ctx)

	prov := testutil.NewFakeProvider()
	pool := agent.NewPool()
	pool.SetProvider(prov)
	pool.SetDefaultModelName("fake-model")

	lm, _ := pool.LanguageModelForModel(ctx, "fake-model")
	_, _ = pool.Spawn("main", lm, agent.SpawnOpts{})

	r := &fakeRenderer{}
	sess := session.Wire(h, pool, "main", r)
	if sess == nil {
		t.Fatal("Wire returned nil session")
	}
}

func TestWire_Start_FiresSessionStart(t *testing.T) {
	ctx := context.Background()
	h := extension.NewHost(nil)
	defer h.Close(ctx)

	prov := testutil.NewFakeProvider()
	pool := agent.NewPool()
	pool.SetProvider(prov)
	pool.SetDefaultModelName("fake-model")

	lm, _ := pool.LanguageModelForModel(ctx, "fake-model")
	_, _ = pool.Spawn("main", lm, agent.SpawnOpts{})

	r := &fakeRenderer{}
	sess := session.Wire(h, pool, "main", r)

	// Start should complete without error (no WASM extensions loaded).
	if err := sess.Start(ctx); err != nil {
		t.Fatalf("Start: %v", err)
	}
}

func TestWire_Cancel_NoopWhenNotStreaming(t *testing.T) {
	// Should not panic when Cancel is called without an active turn.
	ctx := context.Background()
	h := extension.NewHost(nil)
	defer h.Close(ctx)

	pool := agent.NewPool()
	r := &fakeRenderer{}
	sess := session.Wire(h, pool, "main", r)
	sess.Cancel() // must not panic
}

func TestWire_NilHost_NoopExtensionCalls(t *testing.T) {
	// session.Wire with nil host must return a usable Session.
	pool := agent.NewPool()
	r := &fakeRenderer{}
	sess := session.Wire(nil, pool, "main", r)
	if sess == nil {
		t.Fatal("Wire with nil host returned nil")
	}
	if err := sess.Start(context.Background()); err != nil {
		t.Fatalf("Start with nil host: %v", err)
	}
	sess.Cancel() // must not panic
}

func TestWire_Close_WithNilPool(t *testing.T) {
	ctx := context.Background()
	h := extension.NewHost(nil)
	defer func() { _ = h.Close(ctx) }()

	sess := session.Wire(h, nil, "main", &fakeRenderer{})
	// Close should not panic with nil pool.
	if err := sess.Close(ctx); err != nil {
		t.Fatalf("Close: %v", err)
	}
}
