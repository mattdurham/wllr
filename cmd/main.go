package main

import (
	"bytes"
	"context"
	"embed"
	"encoding/json"
	yaml "gopkg.in/yaml.v3"
	"flag"
	"fmt"
	"log/slog"
	"net/http"
	"os"
	"path/filepath"
	"time"

	tea "charm.land/bubbletea/v2"
	fantasy "charm.land/fantasy"
	"github.com/mattdurham/wllr/modules/agent"
	"github.com/mattdurham/wllr/modules/extension"
	"github.com/mattdurham/wllr/modules/harness"
	"github.com/mattdurham/wllr/modules/mcp"
	"github.com/mattdurham/wllr/modules/sdk"
	"github.com/mattdurham/wllr/modules/tools"
)

// builtinFS embeds generated built-in WASM modules when they are present.
// `make build` runs `make extensions` first, so release binaries include them.
// Clean checkouts still compile because cmd/builtins contains a tracked README.
// read_file, write_file, exec, and get_env are native Go tools registered via
// RegisterNativeTool; no WASM is needed for stateless I/O.
//
//go:embed builtins/*
var builtinFS embed.FS

func main() { //nolint:gocyclo // main wires CLI, providers, extensions, and TUI callbacks in one startup path.
	if len(os.Args) > 1 && os.Args[1] == "login" {
		os.Exit(runLoginCommand(context.Background(), os.Args[2:], os.Stdout, os.Stderr))
	}

	execPrompt := flag.String("exec", "", "run a single prompt non-interactively and print the response to stdout")
	pprofAddr := flag.String("pprof", defaultPprofAddr(), "listen address for net/http/pprof debug server (use empty string to disable)")
	flag.Parse()

	cfg, err := LoadConfig()
	if err != nil {
		fmt.Fprintln(os.Stderr, "wllr: "+err.Error())
		os.Exit(1)
	}

	ctx := context.Background()
	resolveLocalProviderConfig(ctx, cfg)

	missingAuthEnv, missingAuth := missingProviderAuth(cfg)
	if missingAuth && *execPrompt != "" {
		fmt.Fprintf(os.Stderr, "wllr: %v\n", missingAuthError(cfg.Provider, missingAuthEnv))
		os.Exit(1)
	}

	var fantasyProv fantasy.Provider
	var langModel fantasy.LanguageModel
	if !missingAuth {
		fantasyProv, langModel, err = buildProvider(ctx, cfg)
		if err != nil {
			fmt.Fprintf(os.Stderr, "wllr: %v\n", err)
			os.Exit(1)
		}
	}

	// Create agent pool and spawn the main agent.
	pool := agent.NewPool()
	if fantasyProv != nil {
		pool.SetProvider(fantasyProv)
	}
	pool.SetProviderName(cfg.Provider)
	pool.SetDefaultModelName(cfg.Model)
	if cfg.ContextWindow > 0 {
		pool.SetContextWindow(cfg.ContextWindow)
	}

	if _, spawnErr := pool.Spawn(agent.MainAgentID, langModel, agent.SpawnOpts{TurnTimeout: -1}); spawnErr != nil {
		fmt.Fprintf(os.Stderr, "wllr: spawn main agent: %v\n", spawnErr)
		os.Exit(1)
	}

	// Build extension host — extension logs flow through slog with "extension" attribute.
	h := extension.NewHost(nil)

	// Configure logging now that the host exists. stderr (exec/headless only) stays
	// in core; the rolling log FILE is written by the bundled `logging` WASM
	// extension, fed by the dispatchLogHandler via EventLog. See cmd/loghandler.go.
	cleanupLog := setupLogging(h, *execPrompt == "")
	defer cleanupLog()
	cleanupPprof := startPprofServer(*pprofAddr)
	defer cleanupPprof()
	// Route the host's own diagnostic logs through the configured default handler
	// too (the dispatch handler's reentrancy guard makes this safe).
	h.SetLogger(slog.Default())

	// Wire OS capabilities via the CapabilityProvider interface.
	h.SetCapabilities(newOSCapabilityProvider(pool))

	// Create the harness model BEFORE loading extensions so that
	// OnRegisterCommand (wired in harness.New) is set when _init and
	// session_start handlers call register_command.
	m := harness.New(pool, agent.MainAgentID, h)

	// Register stateless tools as native Go functions — bypasses WASM entirely.
	registerNativeTools(h)
	registerAgentStatusTool(h, pool)

	loadBuiltinExtensions(ctx, h)

	closeMCP := startMCPBridge(ctx, h)
	defer closeMCP()

	// Load extensions from ~/.wllr/extensions/ and WLLR_EXTENSIONS_DIR.
	var extPaths []string
	extPaths = append(extPaths, loadExtensionsFromSubdirs(ctx, h, wllrExtensionsDir())...)
	if cfg.ExtensionsDir != "" && cfg.ExtensionsDir != wllrExtensionsDir() {
		extPaths = append(extPaths, loadExtensionsFlat(ctx, h, cfg.ExtensionsDir)...)
	}

	defer func() {
		if err := h.Close(ctx); err != nil {
			fmt.Fprintf(os.Stderr, "wllr: close extension host: %v\n", err)
		}
	}()

	// Log registered tools so startup issues are visible in the log.
	{
		registered := h.RegisteredTools()
		names := make([]string, 0, len(registered))
		for _, t := range registered {
			names = append(names, t.Tool.Name)
		}
		slog.Info("wllr: extensions ready", "tools", names)
	}

	// --exec mode: run a single prompt non-interactively and exit.
	if *execPrompt != "" {
		runExecMode(ctx, h, langModel, *execPrompt)
		return
	}

	m.SetExtensionPaths(extPaths)

	// Wire the /model picker: list the active provider's models, and switch +
	// persist on selection. The switch rebuilds the main agent's language model,
	// updates the context window, and saves the choice for next launch.
	currentProvider := cfg.Provider
	oauthState := newOAuthLoginState(ctx, pool, cfg.Model)
	m.ProviderListFn = func() []harness.ProviderChoice {
		return []harness.ProviderChoice{
			{ID: providerOpenAI, Name: "ChatGPT", Sublabel: "sign in with a ChatGPT account"},
			{ID: providerAnthropic, Name: "Anthropic", Sublabel: "sign in with a Claude account"},
			{ID: providerLocal, Name: "Local model", Sublabel: localProviderSublabel(cfg)},
		}
	}
	m.SelectProviderFn = func(provider string) (string, bool, error) {
		modelID := defaultModelForProvider(provider)
		if provider == providerLocal {
			models := localModels(ctx, cfg)
			if len(models) > 0 {
				modelID = models[0].ID
			}
		}
		if modelID == "" {
			return "", false, fmt.Errorf("unknown provider %q", provider)
		}
		currentProvider = provider
		cfg.Provider = provider
		cfg.Model = modelID
		pool.SetProviderName(provider)
		pool.SetDefaultModelName(modelID)
		if cw := contextWindowForSelection(provider, modelID, cfg); cw > 0 {
			pool.SetContextWindow(cw)
		}
		if saveErr := saveProvider(provider); saveErr != nil {
			slog.Warn("wllr: could not persist provider selection", "provider", provider, "error", saveErr)
		}
		if saveErr := saveModel(modelID); saveErr != nil {
			slog.Warn("wllr: could not persist model selection", "model", modelID, "error", saveErr)
		}
		oauthState.model = modelID
		switch provider {
		case providerOpenAI, providerAnthropic:
			return modelID, true, nil
		case providerLocal:
			prov, lm, err := buildProvider(ctx, cfg)
			if err != nil {
				return "", false, err
			}
			pool.SetProvider(prov)
			if main := pool.Get(agent.MainAgentID); main != nil {
				main.SetModel(lm, modelID)
			}
			return modelID, false, nil
		default:
			return "", false, fmt.Errorf("unknown provider %q", provider)
		}
	}
	m.ModelListFn = func() []harness.ModelChoice {
		var catalog []modelInfo
		switch currentProvider {
		case providerOpenAI:
			catalog = modelsForOpenAIAuth()
		case providerLocal:
			catalog = localModels(ctx, cfg)
		default:
			catalog = modelsForProvider(currentProvider)
		}
		out := make([]harness.ModelChoice, 0, len(catalog))
		for _, mi := range catalog {
			out = append(out, harness.ModelChoice{ID: mi.ID, Name: mi.Name})
		}
		return out
	}
	m.SelectModelFn = func(modelID string) error {
		lm, lmErr := pool.LanguageModelForModel(ctx, modelID)
		if lmErr != nil {
			return lmErr
		}
		if main := pool.Get(agent.MainAgentID); main != nil {
			main.SetModel(lm, modelID)
		}
		pool.SetDefaultModelName(modelID)
		if cw := contextWindowForSelection(currentProvider, modelID, cfg); cw > 0 {
			pool.SetContextWindow(cw)
		}
		if saveErr := saveModel(modelID); saveErr != nil {
			slog.Warn("wllr: could not persist model selection", "model", modelID, "error", saveErr)
		}
		return nil
	}

	// Wire the /thinking picker: list reasoning levels, and apply + persist on
	// selection. Applying sets the main agent's provider options (mapped to the
	// active provider's native mechanism) for subsequent turns.
	m.ThinkingListFn = func() []harness.ThinkingChoice {
		out := make([]harness.ThinkingChoice, 0, len(thinkingLevels))
		for _, lvl := range thinkingLevels {
			out = append(out, harness.ThinkingChoice{ID: string(lvl), Label: thinkingLevelLabels[lvl]})
		}
		return out
	}
	m.SelectThinkingFn = func(levelID string) error {
		if !isValidThinkingLevel(levelID) {
			return fmt.Errorf("unknown thinking level %q", levelID)
		}
		level := thinkingLevel(levelID)
		po := providerOptionsForThinking(currentProvider, level)
		if main := pool.Get(agent.MainAgentID); main != nil {
			main.SetProviderOptions(po)
		}
		if saveErr := saveThinkingLevel(level); saveErr != nil {
			slog.Warn("wllr: could not persist thinking level", "level", levelID, "error", saveErr)
		}
		return nil
	}

	// Apply the persisted thinking level (if any) to the main agent at startup so
	// the saved reasoning level survives restarts, and reflect it in the status.
	if lvl := savedThinkingLevel(); lvl != thinkingOff {
		if main := pool.Get(agent.MainAgentID); main != nil {
			main.SetProviderOptions(providerOptionsForThinking(currentProvider, lvl))
		}
		m.SetActiveThinking(string(lvl))
	}

	// Apply a stored, valid OAuth token at startup (refreshing if expired) so a
	// prior /login persists across restarts. Anthropic (browser+callback) and
	// Codex/openai (device-code) are supported.
	resolveStartupOAuth(ctx, pool, cfg)

	// First-run provider auth: record the chosen auth method once per provider so
	// the prompt is not shown again. RecordAuthFn persists to the 0600 auth file.
	m.RecordAuthFn = func(provider, method string) error {
		return saveAuthCredential(provider, authCredential{Type: authType(method)})
	}
	// OAuth login flow: begin returns the modal body + a URL to copy; await blocks
	// until login resolves (Anthropic local callback server, or Codex device-code
	// poll); complete exchanges the result and swaps the live provider.
	m.BeginOAuthFn = oauthState.begin
	m.CompleteOAuthFn = oauthState.complete
	m.AwaitOAuthFn = func() (string, bool) {
		input, err := oauthState.await()
		if err != nil {
			slog.Warn("wllr: oauth login failed", "error", err)
			return "", false
		}
		return input, input != ""
	}
	if missingAuth && !cfg.ModelConfigured && !cfg.ProviderConfigured {
		m.SetPendingSetupWizard()
	} else if missingAuth && !hasAuthRecord(currentProvider) {
		m.SetPendingAuthProvider(currentProvider)
	}

	m.SetLogFn(func(level int, msg string) {
		lvl := []slog.Level{slog.LevelDebug, slog.LevelInfo, slog.LevelWarn, slog.LevelError}
		l := slog.LevelError
		if level >= 0 && level < len(lvl) {
			l = lvl[level]
		}
		slog.Log(ctx, l, msg)
	})

	// Session recording (messages + tool calls) is handled by the bundled
	// `history` WASM extension, which writes JSONL under ~/.wllr/sessions/ and
	// provides browse/rollback UI. The former core session.Journal was redundant
	// with it and has been removed.

	prog := tea.NewProgram(&m)
	m.SetProgram(prog)

	if _, err := prog.Run(); err != nil {
		fmt.Fprintln(os.Stderr, "wllr: "+err.Error())
		os.Exit(1)
	}
}

// setupLogging configures the default slog handler. The rolling log FILE is
// written by the bundled `logging` WASM extension (fed via EventLog), not core.
// In exec/headless mode logs also go to stderr; in TUI mode stderr is omitted
// (it would corrupt the alt-screen). Returns a cleanup function (stops the log
// dispatch goroutine) that must be deferred by the caller.
func setupLogging(h *extension.Host, tuiMode bool) func() {
	// stderr handler: only in exec/headless mode (it would corrupt the TUI alt-screen).
	var stderrH slog.Handler
	if !tuiMode {
		stderrH = slog.NewTextHandler(os.Stderr, &slog.HandlerOptions{Level: slog.LevelInfo})
	}
	// File/sink handler: the WASM log dispatcher, forwarding records to the
	// bundled `logging` extension (which writes ~/.wllr/logs/<ts>.log).
	dispatchH, stopDispatch := newDispatchLogHandler(h, slog.LevelDebug)

	slog.SetDefault(slog.New(newTee(stderrH, dispatchH)))
	return stopDispatch
}

// registerAgentStatusTool registers the get_agent_status native tool on h.
// The tool returns turn count and recent conversation history for a running agent.
func registerAgentStatusTool(h *extension.Host, pool *agent.AgentPool) {
	h.RegisterNativeTool(sdk.Tool{
		Name:        "get_agent_status",
		Description: "No-side-effect status check for an agent. Returns is_running, pending_messages, turn_count, and recent history. If is_running=true, the child is working; do not ping it.",
		InputSchema: json.RawMessage(`{"type":"object","properties":{"agent_id":{"type":"string","description":"Agent ID to inspect"},"history_limit":{"type":"integer","description":"Number of recent messages to include (default 10)"}},"required":["agent_id"]}`),
	}, func(_ context.Context, input json.RawMessage) (string, bool) {
		var in struct {
			AgentID      string `json:"agent_id"`
			HistoryLimit int    `json:"history_limit"`
		}
		if err := json.Unmarshal(input, &in); err != nil || in.AgentID == "" {
			return "agent_id is required", true
		}
		if in.HistoryLimit <= 0 {
			in.HistoryLimit = 10
		}
		a := pool.Get(in.AgentID)
		if a == nil {
			return fmt.Sprintf("agent %q not found", in.AgentID), true
		}
		history := a.History()
		start := len(history) - in.HistoryLimit
		if start < 0 {
			start = 0
		}
		recent := history[start:]
		type msgOut struct {
			Role    string `json:"role"`
			Preview string `json:"preview"`
		}
		msgs := make([]msgOut, 0, len(recent))
		for _, m := range recent {
			preview := string([]rune(m.Content))
			if r := []rune(preview); len(r) > 200 {
				preview = string(r[:200]) + "…"
			}
			msgs = append(msgs, msgOut{Role: string(m.Role), Preview: preview})
		}
		out, _ := json.Marshal(map[string]any{
			"agent_id":         in.AgentID,
			"is_running":       a.IsRunning(),
			"pending_messages": a.InboxLen(),
			"turn_count":       len(history) / 2,
			"last_summary":     a.LastSummary(),
			"recent":           msgs,
		})
		return string(out), false
	})
}

// loadBuiltinExtensions loads the trusted built-in WASM extensions.
func loadBuiltinExtensions(ctx context.Context, h *extension.Host) {
	for _, name := range []string{"agents", "history", "logging", "statusline"} {
		filename := name + ".wasm"
		data, err := builtinFS.ReadFile("builtins/" + filename)
		if err != nil {
			slog.Warn("wllr: built-in extension missing; run `make extensions` before building", "extension", name, "error", err)
			continue
		}
		if loadErr := h.LoadBytes(ctx, filename, data, true); loadErr != nil {
			fmt.Fprintf(os.Stderr, "wllr: load built-in extension %q: %v\n", name, loadErr)
		}
	}
}

// startMCPBridge initializes the MCP server bridge and returns a cleanup function.
// Startup errors are non-fatal: the bridge logs a warning and the app continues.
func startMCPBridge(ctx context.Context, h *extension.Host) func() {
	mcpExt := mcp.NewExtension(h)
	if mcpErr := mcpExt.Start(ctx); mcpErr != nil {
		slog.Warn("wllr: mcp bridge init failed", "error", mcpErr)
	}
	return func() {
		if closeErr := mcpExt.Close(); closeErr != nil {
			slog.Warn("wllr: close mcp bridge", "error", closeErr)
		}
	}
}

// runExecMode runs a single prompt non-interactively and exits.
// It builds a one-shot fantasy agent, streams the response to stdout, and calls
// os.Exit(1) on error.
func runExecMode(ctx context.Context, h *extension.Host, langModel fantasy.LanguageModel, prompt string) {
	fantasyTools := tools.BuildFantasyTools(h, "exec", func(level int, msg string) {
		slog.Log(
			ctx,
			[]slog.Level{slog.LevelDebug, slog.LevelInfo, slog.LevelWarn, slog.LevelError}[min(level, 3)],
			msg,
		)
	})
	var agentOpts []fantasy.AgentOption
	if len(fantasyTools) > 0 {
		agentOpts = append(agentOpts, fantasy.WithTools(fantasyTools...))
	}
	if sp := loadSystemPrompt(); sp != "" {
		agentOpts = append(agentOpts, fantasy.WithSystemPrompt(sp))
	}
	fa := fantasy.NewAgent(langModel, agentOpts...)
	_, execErr := fa.Stream(ctx, fantasy.AgentStreamCall{
		Prompt: prompt,
		OnTextDelta: func(_, text string) error {
			fmt.Print(text)
			return nil
		},
	})
	fmt.Println()
	if execErr != nil {
		fmt.Fprintf(os.Stderr, "wllr: %v\n", execErr)
		os.Exit(1)
	}
}

// configPath returns the path to the shared wllr config file.
func configPath() string {
	if p := os.Getenv("WLLR_CONFIG"); p != "" {
		return p
	}
	home, err := os.UserHomeDir()
	if err != nil {
		return ".wllr/config.yaml"
	}
	return filepath.Join(home, ".config", "wllr", "config.yaml")
}

// loadConfigGroup reads the config file and returns the JSON blob for the given group.
// Returns {} if the group does not exist. The config file is a flat YAML object
// keyed by group name (extension name or "wllr" for the main app).
func loadConfigGroup(group string) (json.RawMessage, error) {
	data, err := os.ReadFile(configPath())
	if err != nil {
		if os.IsNotExist(err) {
			return json.RawMessage("{}"), nil
		}
		return nil, err
	}

	// Parse YAML into map
	var all map[string]yaml.Node
	if err := yaml.Unmarshal(data, &all); err != nil {
		return nil, fmt.Errorf("config: parse error: %w", err)
	}
	if v, ok := all[group]; ok {
		// Marshal the node back to JSON bytes
		out, err := yaml.Marshal(v)
		if err != nil {
			return nil, fmt.Errorf("config: marshal error: %w", err)
		}
		return json.RawMessage(out), nil
	}

	return json.RawMessage("{}"), nil
}

// httpPost performs an HTTP POST request and returns the status code and body.
// Extracted from main() to keep cyclomatic complexity within the project limit.
func httpPost(url string, headers map[string]string, body []byte) (int, []byte, error) {
	req, err := http.NewRequest(http.MethodPost, url, bytes.NewReader(body))
