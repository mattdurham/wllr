package main

import (
	"context"
	"fmt"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"testing"

	fantasyopenapiprovider "charm.land/fantasy/providers/openai"
)

// --- provider option mapping ---

func TestProviderOptionsForThinkingMode_Local(t *testing.T) {
	// Local is OpenAI-compatible: named efforts map through verbatim.
	po := providerOptionsForThinkingMode(providerLocal, thinkingModeHigh)
	data, ok := po[fantasyopenapiprovider.Name]
	if !ok {
		t.Fatalf("local high: no openai options, got %v", po)
	}
	opts, ok := data.(*fantasyopenapiprovider.ProviderOptions)
	if !ok || opts.ReasoningEffort == nil {
		t.Fatalf("local high: unexpected options %T %v", data, opts)
	}
	if *opts.ReasoningEffort != fantasyopenapiprovider.ReasoningEffortHigh {
		t.Errorf("effort = %q, want high", *opts.ReasoningEffort)
	}

	// "none" must be sent explicitly on local (the server would otherwise keep
	// its own default, which for LM Studio loaded thinking models is "on").
	po = providerOptionsForThinkingMode(providerLocal, thinkingModeNone)
	opts, ok = po[fantasyopenapiprovider.Name].(*fantasyopenapiprovider.ProviderOptions)
	if !ok || opts.ReasoningEffort == nil || *opts.ReasoningEffort != fantasyopenapiprovider.ReasoningEffortNone {
		t.Errorf("local none: expected explicit ReasoningEffort=none, got %v", opts)
	}

	// Non-standard vocabulary (LM Studio's boolean "on") → none, never an
	// effort the endpoint would reject.
	po = providerOptionsForThinkingMode(providerLocal, "on")
	opts, ok = po[fantasyopenapiprovider.Name].(*fantasyopenapiprovider.ProviderOptions)
	if !ok || opts.ReasoningEffort == nil || *opts.ReasoningEffort != fantasyopenapiprovider.ReasoningEffortNone {
		t.Errorf("local \"on\": expected ReasoningEffort=none, got %v", opts)
	}
}

func TestProviderOptionsForThinkingMode_OpenAI(t *testing.T) {
	// Native openai (incl. Codex): the "none" mode emits an explicit
	// ReasoningEffort=none (a documented value; turns reasoning off).
	po := providerOptionsForThinkingMode(providerOpenAI, thinkingModeNone)
	opts, ok := po[fantasyopenapiprovider.Name].(*fantasyopenapiprovider.ProviderOptions)
	if !ok || opts.ReasoningEffort == nil || *opts.ReasoningEffort != fantasyopenapiprovider.ReasoningEffortNone {
		t.Errorf("openai none: got %v, want explicit none", opts)
	}
	// A standard effort maps through.
	po = providerOptionsForThinkingMode(providerOpenAI, thinkingModeMedium)
	opts, ok = po[fantasyopenapiprovider.Name].(*fantasyopenapiprovider.ProviderOptions)
	if !ok || opts.ReasoningEffort == nil || *opts.ReasoningEffort != fantasyopenapiprovider.ReasoningEffortMedium {
		t.Errorf("openai medium: got %v", opts)
	}
	// A non-standard/unknown ID is omitted (→ nil, no field) so it can never
	// 400 a request on the OpenAI API — this is the pre-existing behavior.
	if po := providerOptionsForThinkingMode(providerOpenAI, "on"); po != nil {
		t.Errorf("openai unknown id = %v, want nil (omit)", po)
	}
}

// --- LM Studio capability mapping ---

func TestLMSReasoningModeID(t *testing.T) {
	tests := []struct {
		opt  string
		want string
		ok   bool
	}{
		{"off", thinkingModeNone, true},
		{"low", thinkingModeLow, true},
		{"medium", thinkingModeMedium, true},
		{"xhigh", thinkingModeXHigh, true},
		{"on", thinkingModeMedium, true}, // boolean → wire-safe "medium"
		{"bogus", "", false},
	}
	for _, tt := range tests {
		got, ok := lmsReasoningModeID(tt.opt)
		if got != tt.want || ok != tt.ok {
			t.Errorf("lmsReasoningModeID(%q) = (%q, %v), want (%q, %v)", tt.opt, got, ok, tt.want, tt.ok)
		}
	}
}

func TestLMSReasoningToModes_Grading(t *testing.T) {
	cap := &lmsReasoningCapability{AllowedOptions: []string{"off", "low", "medium", "xhigh", "on"}, Default: "xhigh"}
	modes := lmsReasoningToModes(cap)
	ids := make([]string, 0, len(modes))
	for _, m := range modes {
		ids = append(ids, m.ID)
	}
	// The boolean "on" maps to medium which is already present (graded), so no
	// duplicate: expect the graded set in declaration order.
	want := []string{thinkingModeNone, thinkingModeLow, thinkingModeMedium, thinkingModeXHigh}
	if fmt.Sprint(ids) != fmt.Sprint(want) {
		t.Errorf("modes = %v, want %v", ids, want)
	}
}

func TestLMSReasoningToModes_BooleanOnly(t *testing.T) {
	cap := &lmsReasoningCapability{AllowedOptions: []string{"off", "on"}, Default: "on"}
	modes := lmsReasoningToModes(cap)
	ids := make([]string, 0, len(modes))
	for _, m := range modes {
		ids = append(ids, m.ID)
	}
	if fmt.Sprint(ids) != fmt.Sprint([]string{thinkingModeNone, thinkingModeMedium}) {
		t.Errorf("boolean-only modes = %v, want [none medium(On)]", ids)
	}
	if len(modes) == 2 && modes[1].Name != "On" {
		t.Errorf("boolean-only enabled label = %q, want On", modes[1].Name)
	}
}

func TestLMSReasoningToModes_Unusable(t *testing.T) {
	if modes := lmsReasoningToModes(&lmsReasoningCapability{AllowedOptions: []string{"bogus"}}); modes != nil {
		t.Errorf("all-bogus options = %v, want nil", modes)
	}
	if modes := lmsReasoningToModes(nil); modes != nil {
		t.Errorf("nil cap = %v, want nil", modes)
	}
}

func TestLMSReasoningDefault(t *testing.T) {
	cap := &lmsReasoningCapability{AllowedOptions: []string{"off", "low", "medium", "xhigh", "on"}, Default: "xhigh"}
	modes := lmsReasoningToModes(cap)
	if got := lmsReasoningDefault(cap, modes); got != thinkingModeXHigh {
		t.Errorf("default = %q, want xhigh", got)
	}
	// A default not in the offered set → "".
	cap2 := &lmsReasoningCapability{AllowedOptions: []string{"off", "on"}, Default: "medium"}
	modes2 := lmsReasoningToModes(cap2)
	if got := lmsReasoningDefault(cap2, modes2); got != "" {
		t.Errorf("out-of-set default = %q, want empty", got)
	}
}

// --- app API endpoint derivation + probe ---

func TestLMSAppModelsEndpoint(t *testing.T) {
	tests := []struct{ base, want string }{
		{"http://localhost:1234/v1", "http://localhost:1234/api/v1/models"},
		{"http://localhost:1234", "http://localhost:1234/api/v1/models"},
		{"http://localhost:1234/api/v1", "http://localhost:1234/api/v1/models"},
		{"  ", ""},
	}
	for _, tt := range tests {
		if got := lmsAppModelsEndpoint(tt.base); got != tt.want {
			t.Errorf("lmsAppModelsEndpoint(%q) = %q, want %q", tt.base, got, tt.want)
		}
	}
}

func TestQueryLMSV1Models(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path != "/api/v1/models" {
			t.Errorf("path = %q, want /api/v1/models", r.URL.Path)
		}
		_, _ = fmt.Fprint(w, `{"models":[
			{"key":"qwen/qwen3.8-27b","capabilities":{"reasoning":{"allowed_options":["off","low","medium","xhigh","on"],"default":"xhigh"}}},
			{"key":"glm-4.5-air","capabilities":{"reasoning":null}},
			{"key":"text-embedding-x"}
		]}`)
	}))
	defer server.Close()

	out := queryLMSV1Models(context.Background(), server.URL+"/api/v1/models", "")
	if len(out) != 2 {
		t.Fatalf("len(out) = %d, want 2 (models with a capabilities object): %v", len(out), out)
	}
	cap, ok := out["qwen/qwen3.8-27b"]
	if !ok {
		t.Fatalf("missing qwen3.8 entry: %v", out)
	}
	if cap.Default != "xhigh" || len(cap.AllowedOptions) != 5 {
		t.Errorf("cap = %+v", cap)
	}
	if glm := out["glm-4.5-air"]; glm != nil {
		t.Errorf("glm-4.5-air (reasoning null) = %+v, want nil cap (declared, non-reasoning)", glm)
	}
}

func TestQueryLMSV1Models_404(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusNotFound)
	}))
	defer server.Close()
	if out := queryLMSV1Models(context.Background(), server.URL+"/api/v1/models", ""); out != nil {
		t.Errorf("404 = %v, want nil", out)
	}
}

// --- full discovery integration (both endpoints on one test server) ---

func TestDiscoverLocalModels_AttachesReasoningFromAppAPI(t *testing.T) {
	var sawAppAPI bool
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		switch r.URL.Path {
		case "/v1/models":
			_, _ = fmt.Fprint(w, `{"data":[{"id":"qwen/qwen3.8-27b"},{"id":"glm-4.5-air"},{"id":"unknown-thing"}]}`)
		case "/api/v1/models":
			sawAppAPI = true
			_, _ = fmt.Fprint(w, `{"models":[
				{"key":"qwen/qwen3.8-27b","capabilities":{"reasoning":{"allowed_options":["off","low","medium","xhigh","on"],"default":"xhigh"}}},
				{"key":"glm-4.5-air","capabilities":{}}
			]}`)
		default:
			t.Errorf("unexpected path %q", r.URL.Path)
		}
	}))
	defer server.Close()

	cfg := &Config{LocalModels: []localModelConfig{{
		ID: "configured-model", BaseURL: server.URL + "/v1",
	}}}
	models := discoverLocalModels(context.Background(), cfg)
	if len(models) != 3 {
		t.Fatalf("len(models) = %d, want 3: %+v", len(models), models)
	}
	byID := make(map[string]modelInfo)
	for _, m := range models {
		byID[m.ID] = m
	}
	qwen := byID["qwen/qwen3.8-27b"]
	if !qwen.ReasoningDeclared {
		t.Error("qwen3.8: ReasoningDeclared = false, want true")
	}
	if got := adoptedLocalThinkingMode(qwen.ThinkingModes, qwen.ReasoningDefault); got != thinkingModeXHigh {
		t.Errorf("qwen3.8 adopted default = %q, want xhigh (the server's declared default)", got)
	}
	if len(qwen.ThinkingModes) != 4 {
		t.Errorf("qwen3.8 modes = %v, want 4 (none/low/medium/xhigh; boolean on dedupes)", qwen.ThinkingModes)
	}
	glm := byID["glm-4.5-air"]
	if !glm.ReasoningDeclared || len(glm.ThinkingModes) != 0 {
		t.Errorf("glm-4.5-air (no reasoning cap): declared=%v modes=%v, want declared with empty set", glm.ReasoningDeclared, glm.ThinkingModes)
	}
	unk := byID["unknown-thing"]
	if unk.ReasoningDeclared {
		t.Error("model absent from the app API listing must not be ReasoningDeclared")
	}
	if !sawAppAPI {
		t.Error("expected a probe of /api/v1/models")
	}
}

func TestDiscoverLocalModels_RespectsExplicitConfigModes(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		switch r.URL.Path {
		case "/v1/models":
			_, _ = fmt.Fprint(w, `{"data":[{"id":"qwen/qwen3.8-27b"}]}`)
		case "/api/v1/models":
			// App API claims a wider set — explicit config must win.
			_, _ = fmt.Fprint(w, `{"models":[{"key":"qwen/qwen3.8-27b","capabilities":{"reasoning":{"allowed_options":["off","low","medium","xhigh","on"],"default":"xhigh"}}}]}`)
		default:
			t.Errorf("unexpected path %q", r.URL.Path)
		}
	}))
	defer server.Close()

	cfg := &Config{LocalModels: []localModelConfig{{
		ID: "qwen/qwen3.8-27b", BaseURL: server.URL + "/v1",
		ThinkingModes: []string{"none", "low"},
	}}}
	models := discoverLocalModels(context.Background(), cfg)
	if len(models) != 1 {
		t.Fatalf("len(models) = %d, want 1", len(models))
	}
	ids := make([]string, 0)
	for _, m := range models[0].ThinkingModes {
		ids = append(ids, m.ID)
	}
	if fmt.Sprint(ids) != fmt.Sprint([]string{thinkingModeNone, thinkingModeLow}) {
		t.Errorf("explicit config modes = %v, want [none low] (config overrides endpoint)", ids)
	}
}

// --- local thinking resolution (config file + endpoint) ---

// resetLocalThinkingState isolates the package-level resolution cache between
// subtests.
func resetLocalThinkingState() {
	localThinkingState.mu.Lock()
	localThinkingState.key = ""
	localThinkingState.resolved = false
	localThinkingState.mu.Unlock()
}

func TestLocalThinkingInfo_DeclaredEndpoint(t *testing.T) {
	resetLocalThinkingState()
	defer resetLocalThinkingState()

	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		switch r.URL.Path {
		case "/v1/models":
			_, _ = fmt.Fprint(w, `{"data":[{"id":"qwen/qwen3.8-27b"}]}`)
		case "/api/v1/models":
			_, _ = fmt.Fprint(w, `{"models":[{"key":"qwen/qwen3.8-27b","capabilities":{"reasoning":{"allowed_options":["off","low","medium","xhigh","on"],"default":"xhigh"}}}]}`)
		default:
			t.Errorf("unexpected path %q", r.URL.Path)
		}
	}))
	defer server.Close()

	cfg := &Config{Provider: providerLocal, Model: "qwen/qwen3.8-27b", LocalBaseURL: server.URL + "/v1",
		LocalModels: []localModelConfig{{ID: "qwen/qwen3.8-27b", BaseURL: server.URL + "/v1"}}}
	modes, declared, def := localThinkingInfo(context.Background(), cfg)
	if !declared {
		t.Error("declared = false, want true")
	}
	if def != thinkingModeXHigh {
		t.Errorf("declared default = %q, want xhigh", def)
	}
	if got := adoptedLocalThinkingMode(modes, def); got != thinkingModeXHigh {
		t.Errorf("adopted = %q, want xhigh", got)
	}
}

func TestLocalThinkingInfo_NoAppAPI(t *testing.T) {
	resetLocalThinkingState()
	defer resetLocalThinkingState()

	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		switch r.URL.Path {
		case "/v1/models":
			_, _ = fmt.Fprint(w, `{"data":[{"id":"some-model"}]}`)
		default:
			w.WriteHeader(http.StatusNotFound) // any non-LM-Studio server
		}
	}))
	defer server.Close()

	cfg := &Config{Provider: providerLocal, Model: "some-model", LocalBaseURL: server.URL + "/v1",
		LocalModels: []localModelConfig{{ID: "some-model", BaseURL: server.URL + "/v1"}}}
	modes, declared, _ := localThinkingInfo(context.Background(), cfg)
	if declared {
		t.Error("declared = true for a server without the app API, want false")
	}
	if len(modes) != 6 {
		t.Errorf("fallback modes = %v, want the 6 standard OpenAI efforts", modes)
	}
}

func TestLocalThinkingInfo_ExplicitConfigWins(t *testing.T) {
	resetLocalThinkingState()
	defer resetLocalThinkingState()

	// Point the config file at a temp location with explicit thinking_modes.
	dir := t.TempDir()
	path := filepath.Join(dir, "config.yaml")
	if err := os.WriteFile(path, []byte(`wllr:
  local_models:
    - id: qwen/qwen3.8-27b
      base_url: http://ignored.invalid/v1
      thinking_modes: ["none", "medium"]
`), 0o600); err != nil {
		t.Fatal(err)
	}
	t.Setenv("WLLR_CONFIG", path)

	cfg := &Config{Provider: providerLocal, Model: "qwen/qwen3.8-27b", LocalBaseURL: "http://ignored.invalid/v1",
		LocalModels: []localModelConfig{{ID: "qwen/qwen3.8-27b", BaseURL: "http://ignored.invalid/v1", ThinkingModes: []string{"none", "medium"}}}}
	modes, declared, _ := localThinkingInfo(context.Background(), cfg)
	if declared {
		t.Error("explicit config must not report endpoint-declared")
	}
	ids := make([]string, 0)
	for _, m := range modes {
		ids = append(ids, m.ID)
	}
	if fmt.Sprint(ids) != fmt.Sprint([]string{thinkingModeNone, thinkingModeMedium}) {
		t.Errorf("modes = %v, want [none medium] from config", ids)
	}
}

func TestStartupThinkingMode(t *testing.T) {
	// Non-local provider: a valid persisted mode is returned as-is.
	if got := startupThinkingMode(context.Background(), &Config{Provider: providerOpenAI, Model: "gpt-5.5"}, providerOpenAI); got != "" {
		// No config file here (WLLR_CONFIG unset in this subtest) → no persisted
		// mode → "".
		t.Errorf("no persisted mode: got %q, want empty", got)
	}

	// Local, declared endpoint: no persisted mode → the declared default.
	resetLocalThinkingState()
	defer resetLocalThinkingState()
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		switch r.URL.Path {
		case "/v1/models":
			_, _ = fmt.Fprint(w, `{"data":[{"id":"qwen/qwen3.8-27b"}]}`)
		case "/api/v1/models":
			_, _ = fmt.Fprint(w, `{"models":[{"key":"qwen/qwen3.8-27b","capabilities":{"reasoning":{"allowed_options":["off","low","medium","xhigh","on"],"default":"xhigh"}}}]}`)
		default:
			t.Errorf("unexpected path %q", r.URL.Path)
		}
	}))
	defer server.Close()
	cfg := &Config{Provider: providerLocal, Model: "qwen/qwen3.8-27b", LocalBaseURL: server.URL + "/v1",
		LocalModels: []localModelConfig{{ID: "qwen/qwen3.8-27b", BaseURL: server.URL + "/v1"}}}
	if got := startupThinkingMode(context.Background(), cfg, providerLocal); got != thinkingModeXHigh {
		t.Errorf("startup mode = %q, want xhigh (server-declared default)", got)
	}
}
