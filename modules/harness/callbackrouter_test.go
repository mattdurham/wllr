package harness

// NOTE: Any changes to this file must be reflected in the corresponding SPECS.md or NOTES.md.

import (
	"context"
	"encoding/json"
	"errors"
	"testing"
	"time"

	"github.com/mattdurham/wllr/modules/extension"
	"github.com/mattdurham/wllr/modules/sdk"
)

// TestExtensionRouter_DispatchesExtensionCallback proves an extension-owned
// callback reaches the host as an EventOnCommand with the callback name and
// args intact — the return path a browser's agent-tree selection uses.
func TestExtensionRouter_DispatchesExtensionCallback(t *testing.T) {
	host := extension.NewHost(nil)
	ctx := context.Background()
	defer func() { _ = host.Close(ctx) }()

	got := make(chan sdk.OnCommandPayload, 1)
	host.Bus.Subscribe(sdk.EventOnCommand, func(_ context.Context, evt sdk.Event) error {
		var p sdk.OnCommandPayload
		if err := json.Unmarshal(evt.Payload, &p); err != nil {
			return err
		}
		got <- p
		return nil
	})

	r := NewExtensionRouter(host, nil)
	if err := r.RouteCallback(ctx, AgentTreeCallback, []string{"main/coder"}); err != nil {
		t.Fatalf("route: %v", err)
	}

	select {
	case p := <-got:
		if p.Name != AgentTreeCallback || len(p.Args) != 1 || p.Args[0] != "main/coder" {
			t.Fatalf("payload = %+v, want %q [main/coder]", p, AgentTreeCallback)
		}
	case <-time.After(2 * time.Second):
		t.Fatal("extension callback never reached the host")
	}
}

// TestExtensionRouter_CoreCallbackGoesToCoreHandler proves the core/extension
// split: a "__wllr:" callback is handed to the core handler and is NOT
// dispatched to the host as an extension command.
func TestExtensionRouter_CoreCallbackGoesToCoreHandler(t *testing.T) {
	host := extension.NewHost(nil)
	ctx := context.Background()
	defer func() { _ = host.Close(ctx) }()

	dispatched := make(chan struct{}, 1)
	host.Bus.Subscribe(sdk.EventOnCommand, func(context.Context, sdk.Event) error {
		dispatched <- struct{}{}
		return nil
	})

	var coreCB string
	var coreArgs []string
	r := NewExtensionRouter(host, sdk.CoreCallbackFunc(func(_ context.Context, callback string, args []string) error {
		coreCB, coreArgs = callback, args
		return nil
	}))

	if err := r.RouteCallback(ctx, "__wllr:thinking", []string{"high"}); err != nil {
		t.Fatalf("route: %v", err)
	}
	if coreCB != "__wllr:thinking" || len(coreArgs) != 1 || coreArgs[0] != "high" {
		t.Fatalf("core handler got %q %v", coreCB, coreArgs)
	}

	select {
	case <-dispatched:
		t.Fatal("core callback must not be dispatched to the extension host")
	case <-time.After(100 * time.Millisecond):
	}
}

func TestExtensionRouter_NilCoreHandlerSentinel(t *testing.T) {
	r := NewExtensionRouter(extension.NewHost(nil), nil)
	if err := r.RouteCallback(context.Background(), "__wllr:model", nil); !errors.Is(err, sdk.ErrNoCoreHandler) {
		t.Fatalf("err = %v, want ErrNoCoreHandler", err)
	}
}

func TestExtensionRouter_NilHostSentinel(t *testing.T) {
	r := NewExtensionRouter(nil, nil)
	if err := r.RouteCallback(context.Background(), AgentTreeCallback, nil); !errors.Is(err, sdk.ErrNoExtensionHost) {
		t.Fatalf("err = %v, want ErrNoExtensionHost", err)
	}
}

func TestExtensionRouter_EmptyCallback(t *testing.T) {
	r := NewExtensionRouter(extension.NewHost(nil), nil)
	if err := r.RouteCallback(context.Background(), "", nil); !errors.Is(err, sdk.ErrNoCallbackRouter) {
		t.Fatalf("err = %v, want ErrNoCallbackRouter", err)
	}
}

// SetCoreCallbackHandler lets serve mode attach the core handler after the
// router is built, without rebuilding it.
func TestExtensionRouter_SetCoreCallbackHandler(t *testing.T) {
	r := NewExtensionRouter(nil, nil)
	if err := r.RouteCallback(context.Background(), "__wllr:model", nil); !errors.Is(err, sdk.ErrNoCoreHandler) {
		t.Fatalf("before install: err = %v, want ErrNoCoreHandler", err)
	}
	var called bool
	r.SetCoreCallbackHandler(sdk.CoreCallbackFunc(func(context.Context, string, []string) error {
		called = true
		return nil
	}))
	if err := r.RouteCallback(context.Background(), "__wllr:model", nil); err != nil {
		t.Fatalf("after install: %v", err)
	}
	if !called {
		t.Fatal("installed core handler was not called")
	}
}

// The router must satisfy the renderer-neutral contract serve mode installs.
var _ sdk.UICallbackRouter = (*ExtensionRouter)(nil)
