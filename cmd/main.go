package main

import (
	"context"
	_ "embed"
	"encoding/json"
	"flag"
	"fmt"
	"log/slog"
	"os"
	"os/exec"
	"path/filepath"

	tea "charm.land/bubbletea/v2"
	fantasy "charm.land/fantasy"
	fantasyanthropicprovider "charm.land/fantasy/providers/anthropic"
	fantasygoogleprovider "charm.land/fantasy/providers/google"
	fantasyopenapiprovider "charm.land/fantasy/providers/openai"
	"github.com/mattdurham/wllr/agent"
	"github.com/mattdurham/wllr/extension"
	"github.com/mattdurham/wllr/harness"
	"github.com/mattdurham/wllr/mcp"
)

// Built-in extension WASM modules embedded at compile time.
// These are trusted extensions that receive all permissions automatically.
//
//go:embed builtins/readfile.wasm
var readfileWASM []byte

//go:embed builtins/writefile.wasm
var writefileWASM []byte

//go:embed builtins/exec.wasm
var execWASM []byte

//go:embed builtins/env.wasm
var envWASM []byte

//go:embed builtins/agents.wasm
var agentsWASM []byte

func main() {
	execPrompt := flag.String("exec", "", "run a single prompt non-interactively and print the response to stdout")
	flag.Parse()

	cfg, err := LoadConfig()
	if err != nil {
		fmt.Fprintln(os.Stderr, "wllr: "+err.Error())
		os.Exit(1)
	}

	ctx := context.Background()

	// Build fantasy provider based on configured provider name.
	var fantasyProv fantasy.Provider
	var provErr error

	switch cfg.Provider {
	case "anthropic":
		fantasyProv, provErr = fantasyanthropicprovider.New(
			fantasyanthropicprovider.WithAPIKey(cfg.AnthropicAPIKey),
		)
	case "openai":
		fantasyProv, provErr = fantasyopenapiprovider.New(
			fantasyopenapiprovider.WithAPIKey(cfg.OpenAIAPIKey),
		)
	case "gemini":
		fantasyProv, provErr = fantasygoogleprovider.New(
			fantasygoogleprovider.WithGeminiAPIKey(cfg.GeminiAPIKey),
		)
	default:
		fmt.Fprintf(os.Stderr, "wllr: unknown provider %q\n", cfg.Provider)
		os.Exit(1)
	}

	if provErr != nil {
		fmt.Fprintf(os.Stderr, "wllr: create provider: %v\n", provErr)
		os.Exit(1)
	}

	langModel, provErr := fantasyProv.LanguageModel(ctx, cfg.Model)
	if provErr != nil {
		fmt.Fprintf(os.Stderr, "wllr: get language model %q from provider %q: %v\n", cfg.Model, cfg.Provider, provErr)
		os.Exit(1)
	}

	// In TUI mode stderr is suppressed (it bleeds into alt-screen).
	// In --exec mode stderr stays on so the user can see output.
	tuiMode := *execPrompt == ""
	slog.SetDefault(slog.New(newTeeHandler(os.Stderr, !tuiMode)))

	// Create agent pool and spawn the main agent.
	pool := agent.NewPool()
	pool.SetProvider(fantasyProv)
	pool.SetProviderName(cfg.Provider)
	pool.SetDefaultModelName(cfg.Model)

	if _, spawnErr := pool.Spawn("main", langModel, agent.SpawnOpts{}); spawnErr != nil {
		fmt.Fprintf(os.Stderr, "wllr: spawn main agent: %v\n", spawnErr)
		os.Exit(1)
	}

	// Build extension host — extension logs flow through slog with "extension" attribute.
	h := extension.NewHost(nil)

	// Wire host capabilities.
	h.OnExec = func(command, dir string) (string, error) {
		if dir == "" {
			dir = "."
		}
		cmd := exec.Command("sh", "-c", command)
		cmd.Dir = dir
		out, err := cmd.CombinedOutput()
		return string(out), err
	}
	h.OnGetEnv = func(name string) (string, error) {
		if name != "" {
			return os.Getenv(name), nil
		}
		vars := os.Environ()
		data, _ := json.Marshal(vars)
		return string(data), nil
	}
	h.OnConfigRead = loadConfigGroup
	// Apply system prompt changes to ALL agents so sub-agents stay in sync.
	h.OnSetSystemPrompt = func(prompt string) {
		pool.SetBaseSystemPrompt(prompt)
	}
	h.OnAppendSystemPrompt = func(text string) {
		pool.AppendBaseSystemPrompt(text)
	}

	// Create the harness model BEFORE loading extensions so that
	// OnRegisterCommand (wired in harness.New) is set when _init and
	// session_start handlers call register_command.
	m := harness.New(pool, "main", h)

	// Load built-in trusted extensions first.
	builtins := []struct {
		name string
		data []byte
	}{
		{"readfile", readfileWASM},
		{"writefile", writefileWASM},
		{"exec", execWASM},
		{"env", envWASM},
		{"agents", agentsWASM},
	}
	for _, b := range builtins {
		if loadErr := h.LoadBytes(ctx, b.name+".wasm", b.data, true); loadErr != nil {
			fmt.Fprintf(os.Stderr, "wllr: load built-in extension %q: %v\n", b.name, loadErr)
		}
	}
	// Initialize MCP bridge for MCP server tool integration.
	mcpExt := mcp.NewExtension(h)
	if mcpErr := mcpExt.Start(ctx); mcpErr != nil {
		// Non-fatal: log and continue if MCP bridge fails to start.
		slog.Warn("wllr: mcp bridge init failed", "error", mcpErr)
	}
	defer func() {
		if closeErr := mcpExt.Close(); closeErr != nil {
			slog.Warn("wllr: close mcp bridge", "error", closeErr)
		}
	}()



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
		fantasyTools := harness.BuildFantasyTools(h, "exec", func(level int, msg string) {
			slog.Log(ctx, []slog.Level{slog.LevelDebug, slog.LevelInfo, slog.LevelWarn, slog.LevelError}[min(level, 3)], msg)
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
			Prompt: *execPrompt,
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
		return
	}

	m.SetExtensionPaths(extPaths)
	m.SetLogFn(func(level int, msg string) {
		lvl := []slog.Level{slog.LevelDebug, slog.LevelInfo, slog.LevelWarn, slog.LevelError}
		l := slog.LevelError
		if level >= 0 && level < len(lvl) {
			l = lvl[level]
		}
		slog.Log(ctx, l, msg)
	})

	prog := tea.NewProgram(&m)
	m.SetProgram(prog)

	if _, err := prog.Run(); err != nil {
		fmt.Fprintln(os.Stderr, "wllr: "+err.Error())
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
		return ".wllr/config.json"
	}
	return filepath.Join(home, ".config", "wllr", "config.json")
}

// loadConfigGroup reads the config file and returns the JSON blob for the given group.
// Returns {} if the group does not exist. The config file is a flat JSON object
// keyed by group name (extension name or "wllr" for the main app).
func loadConfigGroup(group string) (json.RawMessage, error) {
	data, err := os.ReadFile(configPath())
	if err != nil {
		if os.IsNotExist(err) {
			return json.RawMessage("{}"), nil
		}
		return nil, err
	}
	var all map[string]json.RawMessage
	if err := json.Unmarshal(data, &all); err != nil {
		return nil, fmt.Errorf("config: parse error: %w", err)
	}
	if v, ok := all[group]; ok {
		return v, nil
	}
	return json.RawMessage("{}"), nil
}
