package sdk

// NOTE: Any changes to this file must be reflected in the corresponding SPECS.md or NOTES.md.

import (
	"context"
	"errors"
	"testing"
)

func TestIsCoreCallback(t *testing.T) {
	if !IsCoreCallback("__wllr:model") {
		t.Fatal("__wllr:model should be a core callback")
	}
	if IsCoreCallback("agents:focus") {
		t.Fatal("agents:focus should not be a core callback")
	}
	if IsCoreCallback("") {
		t.Fatal("empty callback should not be a core callback")
	}
}

func TestFuncRouter_RoutesCoreAndExtension(t *testing.T) {
	var coreCB, extCB string
	var coreArgs, extArgs []string
	r := &FuncRouter{
		Core: func(_ context.Context, callback string, args []string) error {
			coreCB, coreArgs = callback, args
			return nil
		},
		Extension: func(_ context.Context, callback string, args []string) error {
			extCB, extArgs = callback, args
			return nil
		},
	}
	ctx := context.Background()

	if err := r.RouteCallback(ctx, "__wllr:model", []string{"gpt-x"}); err != nil {
		t.Fatalf("core route: %v", err)
	}
	if coreCB != "__wllr:model" || len(coreArgs) != 1 || coreArgs[0] != "gpt-x" {
		t.Fatalf("core got %q %v", coreCB, coreArgs)
	}

	if err := r.RouteCallback(ctx, "agents:focus", []string{"main/coder"}); err != nil {
		t.Fatalf("ext route: %v", err)
	}
	if extCB != "agents:focus" || len(extArgs) != 1 || extArgs[0] != "main/coder" {
		t.Fatalf("ext got %q %v", extCB, extArgs)
	}
}

func TestFuncRouter_NilHandlersReturnSentinels(t *testing.T) {
	r := &FuncRouter{}
	ctx := context.Background()

	if err := r.RouteCallback(ctx, "__wllr:thinking", []string{"high"}); !errors.Is(err, ErrNoCoreHandler) {
		t.Fatalf("core with nil handler = %v, want ErrNoCoreHandler", err)
	}
	if err := r.RouteCallback(ctx, "agents:kill", []string{"main/x"}); !errors.Is(err, ErrNoExtensionHost) {
		t.Fatalf("ext with nil handler = %v, want ErrNoExtensionHost", err)
	}
}

// A handler error must propagate unchanged so a caller can distinguish a
// genuine failure from the "unsupported here" sentinels.
func TestFuncRouter_PropagatesHandlerError(t *testing.T) {
	boom := errors.New("boom")
	r := &FuncRouter{Core: func(context.Context, string, []string) error { return boom }}
	if err := r.RouteCallback(context.Background(), "__wllr:model", nil); !errors.Is(err, boom) {
		t.Fatalf("err = %v, want boom", err)
	}
}

func TestCoreCallbackFunc_Adapts(t *testing.T) {
	var got string
	var h CoreCallbackHandler = CoreCallbackFunc(func(_ context.Context, callback string, _ []string) error {
		got = callback
		return nil
	})
	if err := h.HandleCoreCallback(context.Background(), "__wllr:speed", nil); err != nil {
		t.Fatalf("handle: %v", err)
	}
	if got != "__wllr:speed" {
		t.Fatalf("got %q", got)
	}
}
