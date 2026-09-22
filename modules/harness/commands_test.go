package harness

import (
	"strings"
	"testing"

	tea "charm.land/bubbletea/v2"
)

func TestRegistry_Register_And_List(t *testing.T) {
	r := NewRegistry()
	r.Register(Command{Name: "foo", Desc: "do foo", Handler: func(_ []string) tea.Cmd { return nil }})
	r.Register(Command{Name: "bar", Desc: "do bar", Handler: func(_ []string) tea.Cmd { return nil }})

	cmds := r.List()
	if len(cmds) != 2 {
		t.Errorf("expected 2 commands, got %d", len(cmds))
	}
	// Should be sorted.
	if cmds[0].Name != "bar" || cmds[1].Name != "foo" {
		t.Errorf("unexpected order: %v %v", cmds[0].Name, cmds[1].Name)
	}
}

func TestRegistry_Dispatch_KnownCommand(t *testing.T) {
	r := NewRegistry()
	called := false
	r.Register(Command{
		Name: "greet",
		Desc: "say hello",
		Handler: func(args []string) tea.Cmd {
			called = true
			return nil
		},
	})

	cmd := r.Dispatch("greet", nil)
	if cmd != nil {
		cmd() // Execute the returned Cmd.
	}
	if !called {
		t.Error("expected handler to be called")
	}
}

func TestRegistry_Dispatch_UnknownCommand(t *testing.T) {
	r := NewRegistry()
	cmd := r.Dispatch("nonexistent", nil)
	if cmd == nil {
		t.Fatal("expected non-nil Cmd for unknown command")
	}
	msg := cmd()
	notify, ok := msg.(NotifyMsg)
	if !ok {
		t.Fatalf("expected NotifyMsg, got %T", msg)
	}
	if notify.Text == "" {
		t.Error("expected non-empty error message")
	}
}

func TestBuiltinHelp(t *testing.T) {
	r := NewRegistry()
	registerBuiltins(r)
	// /help is handled directly in model.go, but let's also confirm it's registered.
	cmds := r.List()
	names := make(map[string]bool, len(cmds))
	for _, c := range cmds {
		names[c.Name] = true
	}
	for _, expected := range []string{"help", "clear", "reload", "model", "models", "history"} {
		if !names[expected] {
			t.Errorf("expected builtin command %q to be registered", expected)
		}
	}
}

func TestBuiltinHistory_DispatchesExtensionEvent(t *testing.T) {
	r := NewRegistry()
	registerBuiltins(r)

	cmd := r.Dispatch("history", nil)
	if cmd == nil {
		t.Fatal("expected non-nil command")
	}
	msg, ok := cmd().(dispatchOnCommandMsg)
	if !ok {
		t.Fatalf("expected dispatchOnCommandMsg, got %T", cmd())
	}
	if msg.Name != "history" {
		t.Fatalf("command name = %q, want history", msg.Name)
	}
}

func TestBuiltinModels_NoArgs(t *testing.T) {
	r := NewRegistry()
	registerBuiltins(r)

	cmd := r.Dispatch("models", nil)
	if cmd == nil {
		t.Fatal("expected non-nil Cmd")
	}
	msg := cmd()
	if _, ok := msg.(showModelPickerMsg); !ok {
		t.Errorf("expected showModelPickerMsg for /models, got %T", msg)
	}
}

func TestOpenModelPicker_UsesChoiceSublabel(t *testing.T) {
	m := newTestModel()
	m.width = 80
	m.height = 24
	m.activeModel = "m1"
	m.ModelListFn = func() []ModelChoice {
		return []ModelChoice{
			{ID: "m1", Name: "Model One", Sublabel: "http://localhost:8000/v1 · 300k ctx", ContextWindowKnown: true},
		}
	}

	m.openModelPicker()

	if !m.picker.IsActive() {
		t.Fatal("picker should be active")
	}
	if len(m.picker.Items) != 1 {
		t.Fatalf("picker item count = %d, want 1", len(m.picker.Items))
	}
	want := "http://localhost:8000/v1 · 300k ctx  (current)"
	if got := m.picker.Items[0].Sublabel; got != want {
		t.Fatalf("picker sublabel = %q, want %q", got, want)
	}
}

func TestBuiltinClear_EmitsMsg(t *testing.T) {
	r := NewRegistry()
	registerBuiltins(r)

	cmd := r.Dispatch("clear", nil)
	if cmd == nil {
		t.Fatal("expected non-nil Cmd")
	}
	msg := cmd()
	if _, ok := msg.(clearMsg); !ok {
		t.Errorf("expected clearMsg, got %T", msg)
	}
}

func TestBuiltinReload_EmitsMsg(t *testing.T) {
	r := NewRegistry()
	registerBuiltins(r)

	cmd := r.Dispatch("reload", nil)
	if cmd == nil {
		t.Fatal("expected non-nil Cmd")
	}
	msg := cmd()
	if _, ok := msg.(ReloadMsg); !ok {
		t.Errorf("expected ReloadMsg, got %T", msg)
	}
}

func TestBuiltinModel_EmitsMsg(t *testing.T) {
	r := NewRegistry()
	registerBuiltins(r)

	cmd := r.Dispatch("model", []string{"claude-haiku-3-5"})
	if cmd == nil {
		t.Fatal("expected non-nil Cmd")
	}
	msg := cmd()
	setModel, ok := msg.(setModelMsg)
	if !ok {
		t.Fatalf("expected setModelMsg, got %T", msg)
	}
	if setModel.Model != "claude-haiku-3-5" {
		t.Errorf("expected model name %q, got %q", "claude-haiku-3-5", setModel.Model)
	}
}

func TestBuiltinModel_NoArgs(t *testing.T) {
	r := NewRegistry()
	registerBuiltins(r)

	cmd := r.Dispatch("model", nil)
	if cmd == nil {
		t.Fatal("expected non-nil Cmd")
	}
	msg := cmd()
	// /model with no args now opens the model picker instead of a usage notice.
	if _, ok := msg.(showModelPickerMsg); !ok {
		t.Errorf("expected showModelPickerMsg for /model with no args, got %T", msg)
	}
}

func TestBuiltinLogin_OpensProviderWizard(t *testing.T) {
	r := NewRegistry()
	registerBuiltins(r)

	msg := r.Dispatch("login", nil)()
	if _, ok := msg.(showLoginProviderPickerMsg); !ok {
		t.Fatalf("/login message = %T, want showLoginProviderPickerMsg", msg)
	}
}

func TestBuiltinLogin_AuthOpensAuthentication(t *testing.T) {
	r := NewRegistry()
	registerBuiltins(r)

	msg := r.Dispatch("login", []string{"auth"})()
	if _, ok := msg.(loginMsg); !ok {
		t.Fatalf("/login auth message = %T, want loginMsg", msg)
	}
}

func TestBuiltinThinking_EmitsMsg(t *testing.T) {
	r := NewRegistry()
	registerBuiltins(r)

	cmd := r.Dispatch("thinking", []string{"high"})
	if cmd == nil {
		t.Fatal("expected non-nil Cmd")
	}
	msg := cmd()
	set, ok := msg.(setThinkingMsg)
	if !ok {
		t.Fatalf("expected setThinkingMsg, got %T", msg)
	}
	if set.Level != "high" {
		t.Errorf("expected level %q, got %q", "high", set.Level)
	}
}

func TestBuiltinThinking_NoArgs(t *testing.T) {
	r := NewRegistry()
	registerBuiltins(r)

	cmd := r.Dispatch("thinking", nil)
	if cmd == nil {
		t.Fatal("expected non-nil Cmd")
	}
	msg := cmd()
	if _, ok := msg.(showThinkingPickerMsg); !ok {
		t.Errorf("expected showThinkingPickerMsg for /thinking with no args, got %T", msg)
	}
}

func TestRegistry_ExtensionCommand_Callable(t *testing.T) {
	r := NewRegistry()
	var gotArgs []string
	r.Register(Command{
		Name: "ext-hello",
		Desc: "extension hello command",
		Handler: func(args []string) tea.Cmd {
			gotArgs = args
			return nil
		},
	})

	r.Dispatch("ext-hello", []string{"world"})
	if len(gotArgs) != 1 || gotArgs[0] != "world" {
		t.Errorf("expected args [world], got %v", gotArgs)
	}
}

func TestInstantCommandsMarkedInstant(t *testing.T) {
	r := NewRegistry()
	registerBuiltins(r)
	for _, name := range []string{"clear", "reload", "model", "status", "tools"} {
		cmds := r.List()
		found := false
		for _, c := range cmds {
			if c.Name == name {
				found = true
				if !c.Instant {
					t.Errorf("command %q should be Instant=true", name)
				}
				break
			}
		}
		if !found {
			t.Errorf("command %q not found in registry", name)
		}
	}
}

func TestNonInstantCommandDefaultsFalse(t *testing.T) {
	cmd := Command{}
	if cmd.Instant {
		t.Error("Command zero value should have Instant=false")
	}
}

func TestRegistry_Get_ReturnsCommand(t *testing.T) {
	r := NewRegistry()
	r.Register(Command{Name: "myfoo", Desc: "test", Instant: true, Handler: func(_ []string) tea.Cmd { return nil }})
	cmd, ok := r.Get("myfoo")
	if !ok {
		t.Fatal("expected to find command 'myfoo'")
	}
	if !cmd.Instant {
		t.Error("expected Instant=true")
	}
}

func TestRegistry_Get_MissingCommand(t *testing.T) {
	r := NewRegistry()
	_, ok := r.Get("nonexistent")
	if ok {
		t.Error("expected ok=false for nonexistent command")
	}
}

func TestBuiltinModelTiers_ListsTiers(t *testing.T) {
	r := NewRegistry()
	registerBuiltins(r)

	for _, name := range []string{"model", "models"} {
		msg := r.Dispatch(name, []string{"tiers"})()
		if _, ok := msg.(showModelTiersMsg); !ok {
			t.Errorf("/%s tiers message = %T, want showModelTiersMsg", name, msg)
		}
	}
}

func TestApplyModelSelection_TierNameAppliesTier(t *testing.T) {
	m := newTestModel()
	applied := ""
	m.TierNamesFn = func() []string { return []string{"high", "low"} }
	m.ApplyModelTierFn = func(tier string) (string, string, error) {
		applied = tier
		return "anthropic", "claude-opus-4-8", nil
	}
	selectCalled := false
	m.SelectModelFn = func(string) error { selectCalled = true; return nil }

	m.applyModelSelection("high")

	if applied != "high" {
		t.Fatalf("applied tier = %q, want high", applied)
	}
	if selectCalled {
		t.Error("a tier name must not be treated as a model ID")
	}
	if m.activeModel != "claude-opus-4-8" {
		t.Errorf("activeModel = %q, want claude-opus-4-8", m.activeModel)
	}
}

func TestApplyModelSelection_TierLookupIsCaseInsensitive(t *testing.T) {
	m := newTestModel()
	m.TierNamesFn = func() []string { return []string{"high"} }
	got := ""
	m.ApplyModelTierFn = func(tier string) (string, string, error) {
		got = tier
		return "anthropic", "claude-opus-4-8", nil
	}
	m.applyModelSelection("HIGH")
	if got != "high" {
		t.Fatalf("tier = %q, want high", got)
	}
}

func TestApplyModelSelection_PlainModelStillWorks(t *testing.T) {
	m := newTestModel()
	m.TierNamesFn = func() []string { return []string{"high"} }
	selected := ""
	m.SelectModelFn = func(id string) error { selected = id; return nil }
	m.applyModelSelection("claude-haiku-4-5")
	if selected != "claude-haiku-4-5" {
		t.Fatalf("selected = %q, want claude-haiku-4-5", selected)
	}
}

func TestModelPickerTierKeys(t *testing.T) {
	if tier, ok := modelPickerTierKey("h"); !ok || tier != "high" {
		t.Errorf("h → %q/%v, want high/true", tier, ok)
	}
	if tier, ok := modelPickerTierKey("l"); !ok || tier != "low" {
		t.Errorf("l → %q/%v, want low/true", tier, ok)
	}
	if _, ok := modelPickerTierKey("u"); !ok {
		t.Error("u should be a tier key (untag)")
	}
	if _, ok := modelPickerTierKey("enter"); ok {
		t.Error("enter must not be a tier key")
	}
}

func TestOpenModelPicker_ShowsTierTags(t *testing.T) {
	m := newTestModel()
	m.width = 80
	m.height = 24
	m.TagModelTierFn = func(string, string) error { return nil }
	m.ModelListFn = func() []ModelChoice {
		return []ModelChoice{
			{ID: "m1", Name: "Model One", Sublabel: "s", ContextWindowKnown: true, Tiers: []string{"high"}},
		}
	}
	m.openModelPicker()
	if got := m.picker.Items[0].Sublabel; got != "s  tier: high" {
		t.Fatalf("sublabel = %q, want tier tag appended", got)
	}
	if !strings.Contains(m.picker.Title, "h=high") {
		t.Errorf("picker title = %q, want tagging hint", m.picker.Title)
	}
}
