package main

import (
	"context"
	"fmt"
	"net/http"
	"net/http/httptest"
	"os"
	"strings"
	"testing"

	fantasyopenrouterprovider "charm.land/fantasy/providers/openrouter"
)

// withOpenRouterModels writes a wllr config pinning OpenRouter models with the
// given supported_parameters lists (nil omits the field entirely, the legacy
// pre-capture form) and returns a loaded Config with the given model selected.
func withOpenRouterModels(t *testing.T, selected string, params map[string][]string) *Config {
	t.Helper()
	path := withConfigPath(t)
	withAuthPath(t) // hermetic: no stored OpenRouter key from the developer's auth.json
	ids := []string{"reasoning/model", "plain/model", "legacy/model"}
	entries := make([]string, 0, len(ids))
	for _, id := range ids {
		entry := `{"id":"` + id + `","name":"` + id + `","context_window":100000`
		if p, ok := params[id]; ok {
			entry += `,"supported_parameters":[`
			for i, param := range p {
				if i > 0 {
					entry += ","
				}
				entry += `"` + param + `"`
			}
			entry += `]`
		}
		entries = append(entries, entry+`}`)
	}
	cfgJSON := `{"wllr":{"provider":"openrouter","model":"` + selected + `","openrouter_models":[` + strings.Join(entries, ",") + `]}}`
	if err := os.WriteFile(path, []byte(cfgJSON), 0o600); err != nil {
		t.Fatal(err)
	}
	cfg, err := LoadConfig()
	if err != nil {
		t.Fatalf("LoadConfig: %v", err)
	}
	return cfg
}

func TestOpenRouterReasoningCapability(t *testing.T) {
	settings := wllrSettings{OpenRouterModels: []openRouterModelConfig{
		{ID: "reasoning/model", SupportedParameters: []string{"temperature", "reasoning", "top_p"}},
		{ID: "plain/model", SupportedParameters: []string{"temperature"}},
		{ID: "legacy-param/model", SupportedParameters: []string{"include_reasoning"}},
		{ID: "uncaptured/model"},
	}}
	if supports, declared := openRouterReasoningCapability(settings.OpenRouterModels, "reasoning/model"); !supports || !declared {
		t.Errorf("reasoning/model = (%v, %v), want (true, true)", supports, declared)
	}
	if supports, declared := openRouterReasoningCapability(settings.OpenRouterModels, "plain/model"); supports || !declared {
		t.Errorf("plain/model = (%v, %v), want (false, true)", supports, declared)
	}
	if supports, declared := openRouterReasoningCapability(settings.OpenRouterModels, "legacy-param/model"); !supports || !declared {
		t.Errorf("legacy spelling must count as reasoning-capable, got (%v, %v)", supports, declared)
	}
	// No captured data means an older pin: treated as able, not declared.
	if supports, declared := openRouterReasoningCapability(settings.OpenRouterModels, "uncaptured/model"); !supports || declared {
		t.Errorf("uncaptured/model = (%v, %v), want (true, false)", supports, declared)
	}
	if supports, declared := openRouterReasoningCapability(settings.OpenRouterModels, "not-pinned"); !supports || declared {
		t.Errorf("not-pinned = (%v, %v), want (true, false)", supports, declared)
	}
}

func TestProviderOptionsForThinkingModeOpenRouter(t *testing.T) {
	if po := providerOptionsForThinkingMode(providerOpenRouter, "", "reasoning/model"); po != nil {
		t.Errorf("empty mode must clear reasoning, got %v", po)
	}
	// Off maps to enabled:false — an omitted object would leave a thinking
	// model reasoning on its server default.
	po := providerOptionsForThinkingMode(providerOpenRouter, thinkingModeNone, "reasoning/model")
	opts, ok := po[fantasyopenrouterprovider.Name].(*fantasyopenrouterprovider.ProviderOptions)
	if !ok || opts.Reasoning == nil || opts.Reasoning.Enabled == nil || *opts.Reasoning.Enabled {
		t.Fatalf("none: want enabled:false, got %#v", po)
	}
	if opts.Reasoning.Effort != nil {
		t.Errorf("none: effort should be unset, got %v", *opts.Reasoning.Effort)
	}
	// Documented effort IDs pass through verbatim.
	for _, mode := range []string{thinkingModeLow, thinkingModeMedium, thinkingModeHigh} {
		po := providerOptionsForThinkingMode(providerOpenRouter, mode, "reasoning/model")
		opts, ok := po[fantasyopenrouterprovider.Name].(*fantasyopenrouterprovider.ProviderOptions)
		if !ok || opts.Reasoning == nil || opts.Reasoning.Effort == nil {
			t.Fatalf("%s: want reasoning effort, got %#v", mode, po)
		}
		if *opts.Reasoning.Effort != fantasyopenrouterprovider.ReasoningEffort(mode) {
			t.Errorf("%s: effort = %q", mode, *opts.Reasoning.Effort)
		}
		if opts.Reasoning.Enabled != nil {
			t.Errorf("%s: enabled should be unset, got %v", mode, *opts.Reasoning.Enabled)
		}
	}
	// Mode IDs outside OpenRouter's vocabulary (stale saved values from
	// another provider) are omitted rather than sent.
	for _, mode := range []string{thinkingModeMinimal, thinkingModeXHigh, "2048", "on"} {
		if po := providerOptionsForThinkingMode(providerOpenRouter, mode, "reasoning/model"); po != nil {
			t.Errorf("stale mode %q must be omitted, got %v", mode, po)
		}
	}
}

func TestOpenRouterRuntimeOptionsMergeReasoningAndRouting(t *testing.T) {
	withConfigPath(t)
	withAuthPath(t) // hermetic: no stored OpenRouter key
	if err := saveOpenRouterSpeed(""); err != nil {
		t.Fatal(err)
	}
	// Reasoning only: with no routing preference stored, the struct carries
	// just the reasoning options.
	po := openRouterRuntimeProviderOptions(thinkingModeMedium)
	opts := po[fantasyopenrouterprovider.Name].(*fantasyopenrouterprovider.ProviderOptions)
	if opts.Reasoning == nil || opts.Reasoning.Effort == nil || *opts.Reasoning.Effort != fantasyopenrouterprovider.ReasoningEffortMedium {
		t.Fatalf("reasoning-only: %#v", po)
	}
	if opts.Provider != nil {
		t.Errorf("reasoning-only must not carry routing, got %v", opts.Provider)
	}
	// "none" (explicit reasoning off) sends enabled:false but invents no
	// routing preference of its own.
	po = openRouterRuntimeProviderOptions(thinkingModeNone)
	opts = po[fantasyopenrouterprovider.Name].(*fantasyopenrouterprovider.ProviderOptions)
	if opts.Reasoning == nil || opts.Reasoning.Enabled == nil || *opts.Reasoning.Enabled {
		t.Fatalf("none: %#v", po)
	}
	if opts.Provider != nil {
		t.Errorf("none must not inject routing, got %v", opts.Provider)
	}
	// With a routing preference stored, off-mode reasoning keeps it: turning
	// reasoning off disables nothing about routing, and both selections ride
	// in the one struct.
	if err := saveOpenRouterSpeed(openRouterSpeedNitro); err != nil {
		t.Fatal(err)
	}
	po = openRouterRuntimeProviderOptions(thinkingModeNone)
	opts = po[fantasyopenrouterprovider.Name].(*fantasyopenrouterprovider.ProviderOptions)
	if opts.Reasoning == nil || opts.Reasoning.Enabled == nil || *opts.Reasoning.Enabled {
		t.Fatalf("none with routing: %#v", po)
	}
	if opts.Provider == nil || opts.Provider.Sort == nil || *opts.Provider.Sort != openRouterSpeedThroughput {
		t.Errorf("none must keep stored routing, got %v", opts.Provider)
	}
}

func TestSupportedThinkingModesForOpenRouter(t *testing.T) {
	// One listing capture: a reasoning-capable pin, a pin the listing
	// declares unable to reason, and a legacy uncaptured pin.
	cfg := withOpenRouterModels(t, "reasoning/model", map[string][]string{
		"reasoning/model": {"reasoning", "temperature"},
		"plain/model":     {"temperature"},
	})
	modes := supportedThinkingModesForModel(providerOpenRouter, cfg.Model)
	if len(modes) != len(openRouterThinkingModes) {
		t.Fatalf("modes = %v, want %d entries", modes, len(openRouterThinkingModes))
	}
	for _, got := range modes {
		if !thinkingModeInSet(openRouterThinkingModes, got.ID) {
			t.Errorf("unexpected mode %q", got.ID)
		}
	}
	// A model the listing declares unable to reason gets no modes.
	modes = supportedThinkingModesForModel(providerOpenRouter, "plain/model")
	if len(modes) != 0 {
		t.Errorf("plain/model modes = %v, want empty", modes)
	}
	// Pre-capture pins keep the set.
	modes = supportedThinkingModesForModel(providerOpenRouter, "legacy/model")
	if len(modes) != len(openRouterThinkingModes) {
		t.Errorf("legacy/model modes = %v, want %d", modes, len(openRouterThinkingModes))
	}
}

func TestStartupThinkingModeOpenRouterRespectsDeclaration(t *testing.T) {
	// A persisted effort on a model the listing says cannot reason is not
	// applied; the same effort on a reasoning-capable pin is.
	cfg := withOpenRouterModels(t, "plain/model", map[string][]string{
		"plain/model": {"temperature"},
	})
	if err := saveThinkingMode(providerOpenRouter, "plain/model", thinkingModeHigh); err != nil {
		t.Fatal(err)
	}
	if got := startupThinkingMode(context.Background(), cfg, providerOpenRouter); got != "" {
		t.Errorf("declared-no-reasoning model applied mode %q, want none", got)
	}
	cfg = withOpenRouterModels(t, "reasoning/model", map[string][]string{
		"reasoning/model": {"reasoning"},
	})
	if err := saveThinkingMode(providerOpenRouter, "reasoning/model", thinkingModeHigh); err != nil {
		t.Fatal(err)
	}
	if got := startupThinkingMode(context.Background(), cfg, providerOpenRouter); got != thinkingModeHigh {
		t.Errorf("reasoning-capable model lost persisted mode: %q", got)
	}
	// Legacy pins (no captured data) keep the persisted mode too.
	cfg = withOpenRouterModels(t, "legacy/model", nil)
	if err := saveThinkingMode(providerOpenRouter, "legacy/model", thinkingModeHigh); err != nil {
		t.Fatal(err)
	}
	if got := startupThinkingMode(context.Background(), cfg, providerOpenRouter); got != thinkingModeHigh {
		t.Errorf("legacy pin lost persisted mode: %q", got)
	}
}

func TestSavedThinkingModePerModel(t *testing.T) {
	withOpenRouterModels(t, "reasoning/model", nil)
	if err := saveThinkingMode(providerOpenRouter, "reasoning/model", thinkingModeHigh); err != nil {
		t.Fatal(err)
	}
	if err := saveThinkingMode(providerOpenRouter, "legacy/model", thinkingModeMedium); err != nil {
		t.Fatal(err)
	}
	if got := savedThinkingMode(providerOpenRouter, "reasoning/model"); got != thinkingModeHigh {
		t.Errorf("reasoning/model = %q, want %q", got, thinkingModeHigh)
	}
	if got := savedThinkingMode(providerOpenRouter, "legacy/model"); got != thinkingModeMedium {
		t.Errorf("legacy/model = %q, want %q", got, thinkingModeMedium)
	}
	// Models without their own entry inherit the last-saved mode (the legacy
	// global key); other providers never see OpenRouter's entries.
	if got := savedThinkingMode(providerOpenRouter, "plain/model"); got != thinkingModeMedium {
		t.Errorf("plain/model fallback = %q, want %q", got, thinkingModeMedium)
	}
	if got := savedThinkingMode(providerLocal, "reasoning/model"); got != thinkingModeMedium {
		t.Errorf("local/reasoning/model fallback = %q, want %q", got, thinkingModeMedium)
	}
	// Clearing one model's entry leaves the other model's mode saved.
	if err := saveThinkingMode(providerOpenRouter, "reasoning/model", ""); err != nil {
		t.Fatal(err)
	}
	if got := savedThinkingMode(providerOpenRouter, "reasoning/model"); got != "" {
		t.Errorf("reasoning/model after clear = %q, want empty", got)
	}
	if got := savedThinkingMode(providerOpenRouter, "legacy/model"); got != thinkingModeMedium {
		t.Errorf("legacy/model after clear = %q, want %q", got, thinkingModeMedium)
	}
}

func TestStartupThinkingModeOpenRouterPerModel(t *testing.T) {
	// Each model resolves the mode saved for it, not the global last-saved one.
	cfg := withOpenRouterModels(t, "reasoning/model", map[string][]string{
		"reasoning/model": {"reasoning"},
		"plain/model":     {"temperature"},
	})
	if err := saveThinkingMode(providerOpenRouter, "reasoning/model", thinkingModeHigh); err != nil {
		t.Fatal(err)
	}
	if err := saveThinkingMode(providerOpenRouter, "plain/model", thinkingModeMedium); err != nil {
		t.Fatal(err)
	}
	if got := startupThinkingMode(context.Background(), cfg, providerOpenRouter); got != thinkingModeHigh {
		t.Errorf("reasoning/model startup = %q, want %q", got, thinkingModeHigh)
	}
	// A model whose listing declares it unable to reason never gets a mode at
	// startup, even an inherited one.
	cfg = withOpenRouterModels(t, "plain/model", map[string][]string{
		"plain/model":     {"temperature"},
		"reasoning/model": {"reasoning"},
	})
	if got := startupThinkingMode(context.Background(), cfg, providerOpenRouter); got != "" {
		t.Errorf("plain/model startup = %q, want empty (declared unable)", got)
	}
	// A legacy config (only the pre-per-model "thinking_mode" key, no map)
	// still resolves at startup for a model without its own entry.
	cfg = withOpenRouterModels(t, "legacy/model", nil)
	if err := saveWllrField("thinking_mode", thinkingModeMedium); err != nil {
		t.Fatal(err)
	}
	if got := startupThinkingMode(context.Background(), cfg, providerOpenRouter); got != thinkingModeMedium {
		t.Errorf("legacy/model startup = %q, want %q (legacy fallback)", got, thinkingModeMedium)
	}
}

// newOpenRouterModelsServer serves one canned models/user payload.
func newOpenRouterModelsServer(t *testing.T, body string) *httptest.Server {
	t.Helper()
	return httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Header.Get("Authorization") != "Bearer test-key" {
			t.Errorf("missing bearer auth")
		}
		w.Header().Set("Content-Type", "application/json")
		_, _ = fmt.Fprint(w, body)
	}))
}

func TestFetchOpenRouterModelsCapturesSupportedParameters(t *testing.T) {
	server := newOpenRouterModelsServer(t,
		`{"data":[{"id":"a/b","name":"A","context_length":1000,"supported_parameters":["reasoning","temperature"]},{"id":"c/d","context_length":2000}]}`,
	)
	defer server.Close()
	models, err := fetchOpenRouterModels(context.Background(), server.Client(), server.URL, "test-key")
	if err != nil {
		t.Fatalf("fetch: %v", err)
	}
	if len(models) != 2 {
		t.Fatalf("models = %+v", models)
	}
	if got := models[0].SupportedParameters; len(got) != 2 || got[0] != "reasoning" {
		t.Errorf("a/b supported_parameters = %v, want [reasoning temperature]", got)
	}
	if got := models[1].SupportedParameters; got != nil {
		t.Errorf("c/d supported_parameters = %v, want nil", got)
	}
}

// TestStartupThinkingModeRespectsModelVocabulary locks in per-model startup
// resolution: a mode saved for one model applies to another only when the
// other model's own declared vocabulary offers the same name. Models name
// their thinking differently (Anthropic budget IDs, per-family OpenAI effort
// sets, always-on models without a none level), so unguarded cross-model
// inheritance would send modes the target model does not declare — up to
// disabling an always-reasoning model with an inherited "none".
func TestStartupThinkingModeRespectsModelVocabulary(t *testing.T) {
	withConfigPath(t)
	withAuthPath(t) // hermetic: no stored OpenRouter key
	ctx := context.Background()

	// A mode outside the target family's vocabulary never inherits: gpt-5.6
	// declares max (no minimal), gpt-5.5 declares neither.
	if err := saveThinkingMode(providerOpenAI, "gpt-5.6-sol", thinkingModeMax); err != nil {
		t.Fatal(err)
	}
	if got := startupThinkingMode(ctx, &Config{Provider: providerOpenAI, Model: "gpt-5.5"}, providerOpenAI); got != "" {
		t.Errorf("gpt-5.6 max inherited by gpt-5.5 = %q, want nothing", got)
	}
	if got := startupThinkingMode(ctx, &Config{Provider: providerOpenAI, Model: "gpt-5.6-sol"}, providerOpenAI); got != thinkingModeMax {
		t.Errorf("gpt-5.6 own max = %q, want max", got)
	}
	if err := saveThinkingMode(providerOpenAI, "gpt-5.6-sol", thinkingModeMinimal); err != nil {
		t.Fatal(err)
	}
	if got := startupThinkingMode(ctx, &Config{Provider: providerOpenAI, Model: "gpt-5.6-sol"}, providerOpenAI); got != "" {
		t.Errorf("minimal for a model whose set lacks it = %q, want nothing", got)
	}

	// Same-name inheritance stays: the standard efforts are shared across the
	// gpt-5.x family, so a mode saved on gpt-5.5 still restores on gpt-5.4.
	if err := saveThinkingMode(providerOpenAI, "gpt-5.5", thinkingModeHigh); err != nil {
		t.Fatal(err)
	}
	if got := startupThinkingMode(ctx, &Config{Provider: providerOpenAI, Model: "gpt-5.4"}, providerOpenAI); got != thinkingModeHigh {
		t.Errorf("gpt-5.5 high inherited by gpt-5.4 = %q, want high", got)
	}

	// Budget IDs do not cross the Anthropic/OpenAI boundary: "32768" restores
	// on another Anthropic model (same vocabulary) but not on a clean OpenAI
	// model (effort names, no such ID).
	if err := saveThinkingMode(providerAnthropic, claudeOpus48Model, "32768"); err != nil {
		t.Fatal(err)
	}
	if got := startupThinkingMode(ctx, &Config{Provider: providerAnthropic, Model: "claude-sonnet-5"}, providerAnthropic); got != "32768" {
		t.Errorf("anthropic budget inherited by another anthropic model = %q, want 32768", got)
	}
	if got := startupThinkingMode(ctx, &Config{Provider: providerOpenAI, Model: "gpt-5.4-mini"}, providerOpenAI); got != "" {
		t.Errorf("anthropic budget inherited by openai model = %q, want nothing", got)
	}

	// GPT-6 Astra reasons always-on (no none level): a global "none" on the
	// fallback path must not disable it, while the same value restores on a
	// model that does offer none.
	if err := saveThinkingMode(providerOpenAI, "gpt-5.4-mini", thinkingModeNone); err != nil {
		t.Fatal(err)
	}
	if got := startupThinkingMode(ctx, &Config{Provider: providerOpenAI, Model: "gpt-6-astra"}, providerOpenAI); got != "" {
		t.Errorf("global none inherited by always-on astra = %q, want nothing", got)
	}
	if got := startupThinkingMode(ctx, &Config{Provider: providerOpenAI, Model: "gpt-5.4-mini"}, providerOpenAI); got != thinkingModeNone {
		t.Errorf("own none = %q, want none", got)
	}

	// Unknown models resolve against the provider's standard vocabulary: a
	// standard effort saved before the model was catalogued still restores,
	// a nonstandard one does not.
	if err := saveThinkingMode(providerOpenAI, "future-model", thinkingModeHigh); err != nil {
		t.Fatal(err)
	}
	if got := startupThinkingMode(ctx, &Config{Provider: providerOpenAI, Model: "future-model"}, providerOpenAI); got != thinkingModeHigh {
		t.Errorf("unknown model standard mode = %q, want high", got)
	}
	if err := saveThinkingMode(providerOpenAI, "future-model", thinkingModeMax); err != nil {
		t.Fatal(err)
	}
	if got := startupThinkingMode(ctx, &Config{Provider: providerOpenAI, Model: "future-model"}, providerOpenAI); got != "" {
		t.Errorf("unknown model nonstandard mode = %q, want nothing", got)
	}
}

// TestStartupThinkingModeOpenRouterVocabulary: the OpenRouter pinned-model
// gate is the declared-vocabulary check — a mode outside OpenRouter's
// documented set never applies, and a pin declared unable to reason rejects
// everything.
func TestStartupThinkingModeOpenRouterVocabulary(t *testing.T) {
	cfg := withOpenRouterModels(t, "reasoning/model", map[string][]string{
		"reasoning/model": {"temperature", "reasoning"},
		"plain/model":     {"temperature"},
	})
	ctx := context.Background()
	if err := saveThinkingMode(providerOpenRouter, "reasoning/model", thinkingModeXHigh); err != nil {
		t.Fatal(err)
	}
	if got := startupThinkingMode(ctx, &Config{Provider: providerOpenRouter, Model: "reasoning/model"}, providerOpenRouter); got != "" {
		t.Errorf("xhigh on openrouter = %q, want nothing (outside the documented set)", got)
	}
	if err := saveThinkingMode(providerOpenRouter, "reasoning/model", thinkingModeHigh); err != nil {
		t.Fatal(err)
	}
	if got := startupThinkingMode(ctx, &Config{Provider: providerOpenRouter, Model: "reasoning/model"}, providerOpenRouter); got != thinkingModeHigh {
		t.Errorf("own high = %q, want high", got)
	}
	if got := startupThinkingMode(ctx, &Config{Provider: providerOpenRouter, Model: "plain/model"}, providerOpenRouter); got != "" {
		t.Errorf("declared-unable pin = %q, want nothing", got)
	}
	_ = cfg
}
