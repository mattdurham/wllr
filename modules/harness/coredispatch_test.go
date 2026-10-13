package harness

// NOTE: Any changes to this file must be reflected in the corresponding SPECS.md or NOTES.md.

import (
	"context"
	"errors"
	"testing"

	"github.com/mattdurham/wllr/modules/sdk"
)

// TestClassifyCoreSelection pins the callback-id -> apply-action mapping that
// both the terminal and a headless front end consume.
func TestClassifyCoreSelection(t *testing.T) {
	qualified := modelPickerKey("anthropic", "claude-opus-4-8")
	cases := []struct {
		name     string
		callback string
		value    string
		want     coreSelection
	}{
		{"bare model key", modelPickerCallback, "claude-haiku-4-5", coreSelection{kind: coreSelectModel, value: "claude-haiku-4-5"}},
		{"provider-qualified model key", modelPickerCallback, qualified, coreSelection{kind: coreSelectProviderModel, provider: "anthropic", value: "claude-opus-4-8"}},
		{"thinking", thinkingPickerCallback, "high", coreSelection{kind: coreSelectThinking, value: "high"}},
		{"speed", openRouterSpeedPickerCallback, "nitro", coreSelection{kind: coreSelectSpeed, value: "nitro"}},
		{"extension callback", "agents:focus", "[main/coder]", coreSelection{}},
		{"stateful core flow", authPickerCallback, "oauth", coreSelection{}},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			if got := classifyCoreSelection(tc.callback, tc.value); got != tc.want {
				t.Fatalf("classifyCoreSelection(%q, %q) = %+v, want %+v", tc.callback, tc.value, got, tc.want)
			}
		})
	}
}

// TestResolveCoreCallback_ModelSelection proves a POSTed model picker id applies
// through the same SelectModelFn the terminal uses.
func TestResolveCoreCallback_ModelSelection(t *testing.T) {
	m := newTestModel()
	selected := ""
	m.SelectModelFn = func(id string) error { selected = id; return nil }

	handled, err := m.ResolveCoreCallback(context.Background(), modelPickerCallback, []string{"claude-haiku-4-5"})
	if err != nil {
		t.Fatalf("resolve: %v", err)
	}
	if !handled {
		t.Fatal("model selection not handled")
	}
	if selected != "claude-haiku-4-5" {
		t.Fatalf("SelectModelFn got %q, want claude-haiku-4-5", selected)
	}
}

// TestResolveCoreCallback_ProviderModelRouting proves a provider-qualified key
// routes to the cross-provider switch when the provider differs, and to the
// same-provider selection when it matches.
func TestResolveCoreCallback_ProviderModelRouting(t *testing.T) {
	m := newTestModel()
	m.activeProvider = "openai"
	providerSelected := ""
	m.SelectProviderModelFn = func(provider, modelID string) error {
		providerSelected = provider + "/" + modelID
		return nil
	}
	modelSelected := ""
	m.SelectModelFn = func(id string) error { modelSelected = id; return nil }

	handled, err := m.ResolveCoreCallback(context.Background(), modelPickerCallback,
		[]string{modelPickerKey("anthropic", "claude-opus-4-8")})
	if err != nil || !handled {
		t.Fatalf("resolve cross-provider: handled=%v err=%v", handled, err)
	}
	if providerSelected != "anthropic/claude-opus-4-8" {
		t.Fatalf("SelectProviderModelFn got %q, want anthropic/claude-opus-4-8", providerSelected)
	}
	if modelSelected != "" {
		t.Fatalf("SelectModelFn called unexpectedly: %q", modelSelected)
	}

	// A key owned by the active provider stays a same-provider selection. Use a
	// fresh model: the cross-provider switch above updates activeProvider (via
	// setActiveProviderModel), so reusing it would route cross-provider again.
	m2 := newTestModel()
	m2.activeProvider = "openai"
	modelSelected = ""
	m2.SelectModelFn = func(id string) error { modelSelected = id; return nil }

	handled, err = m2.ResolveCoreCallback(context.Background(), modelPickerCallback,
		[]string{modelPickerKey("openai", "gpt-5")})
	if err != nil || !handled {
		t.Fatalf("resolve same-provider: handled=%v err=%v", handled, err)
	}
	if modelSelected != "gpt-5" {
		t.Fatalf("SelectModelFn got %q, want gpt-5", modelSelected)
	}
}

// TestResolveCoreCallback_ThinkingAndSpeed proves the thinking and routing
// selections apply through their callbacks.
func TestResolveCoreCallback_ThinkingAndSpeed(t *testing.T) {
	m := newTestModel()
	thinking := ""
	m.SelectThinkingFn = func(level string) error { thinking = level; return nil }
	speed := ""
	m.SelectSpeedFn = func(id string) error { speed = id; return nil }

	if handled, err := m.ResolveCoreCallback(context.Background(), thinkingPickerCallback, []string{"high"}); err != nil || !handled {
		t.Fatalf("thinking: handled=%v err=%v", handled, err)
	}
	if thinking != "high" {
		t.Fatalf("SelectThinkingFn got %q, want high", thinking)
	}
	if handled, err := m.ResolveCoreCallback(context.Background(), openRouterSpeedPickerCallback, []string{"nitro"}); err != nil || !handled {
		t.Fatalf("speed: handled=%v err=%v", handled, err)
	}
	if speed != "nitro" {
		t.Fatalf("SelectSpeedFn got %q, want nitro", speed)
	}
}

// TestResolveCoreCallback_Unhandled proves an extension callback and a stateful
// setup-flow callback are reported as not handled, so the caller falls through
// to extension dispatch rather than claiming the selection.
func TestResolveCoreCallback_Unhandled(t *testing.T) {
	m := newTestModel()
	for _, callback := range []string{"agents:focus", authPickerCallback, contextWindowCallback} {
		handled, err := m.ResolveCoreCallback(context.Background(), callback, []string{"x"})
		if err != nil {
			t.Fatalf("%s: err = %v", callback, err)
		}
		if handled {
			t.Fatalf("%s: reported handled, want fall-through", callback)
		}
	}
}

// TestCoreCallbackFunc_Sentinel proves the sdk.CoreCallbackHandler adapter
// yields ErrNoCoreHandler for an unhandled callback (so the router answers
// "not implemented here"), and nil for a handled one.
func TestCoreCallbackFunc_Sentinel(t *testing.T) {
	m := newTestModel()
	m.SelectModelFn = func(string) error { return nil }
	handler := m.CoreCallbackFunc()

	if err := handler.HandleCoreCallback(context.Background(), modelPickerCallback, []string{"gpt-5"}); err != nil {
		t.Fatalf("handled callback: err = %v, want nil", err)
	}
	if err := handler.HandleCoreCallback(context.Background(), "agents:focus", []string{"x"}); !errors.Is(err, sdk.ErrNoCoreHandler) {
		t.Fatalf("unhandled callback: err = %v, want ErrNoCoreHandler", err)
	}
}
