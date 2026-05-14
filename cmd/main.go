package main

import (
	"bytes"
	"context"
	_ "embed"
	"encoding/json"
	"flag"
	"fmt"
	"log/slog"
	"net/http"
	"os"
	"os/exec"
	"path/filepath"
	"time"

	tea "charm.land/bubbletea/v2"
	fantasy "charm.land/fantasy"
	"github.com/mattdurham/wllr/agent"
	"github.com/mattdurham/wllr/extension"
	"github.com/mattdurham/wllr/harness"
	"github.com/mattdurham/wllr/mcp"
	"github.com/mattdurham/wllr/sdk"
)

// Built-in extension WASM modules embedded at compile time.
// These are trusted extensions that receive all permissions automatically.
//
// read_file, write_file, exec, and get_env are now native Go tools registered
// directly on the Host via RegisterNativeTool — no WASM needed for stateless I/O.

//go:embed builtins/agents.wasm
var agentsWASM []byte

//go:embed builtins/history.wasm
var historyWASM []byte

func main() {
	execPrompt := flag.String("exec", "", "run a single prompt non-interactively and print the response to stdout")
	flag.Parse()

	cfg, err := LoadConfig()
	if err != nil {
		fmt.Fprintln(os.Stderr, "wllr: "+err.Error())
		os.Exit(1)
	}

	ctx := context.Background()

	fantasyProv, langModel, err := buildProvider(ctx, cfg)
	if err != nil {
		fmt.Fprintf(os.Stderr, "wllr: %v\n", err)
		os.Exit(1)
	}

	// In TUI mode stderr is suppressed (it bleeds into alt-screen).
	// In --exec mode stderr stays on so the user can see output.
	tuiMode := *execPrompt == ""
	var logHandler slog.Handler = newTeeHandler(os.Stderr, !tuiMode)
	if tuiMode {
		// Also write to ~/.wllr/wllr.log so errors are visible after the fact.
		if lf, err := openLogFile(); err == nil {
			logHandler = newTeeHandler(lf, true)
			defer func() {
				if closeErr := lf.Close(); closeErr != nil {
					slog.Warn("wllr: close log file", "error", closeErr)
				}
			}()
		}
	}
	slog.SetDefault(slog.New(logHandler))

	// Create agent pool and spawn the main agent.
	pool := agent.NewPool()
	pool.SetProvider(fantasyProv)
	pool.SetProviderName(cfg.Provider)
	pool.SetDefaultModelName(cfg.Model)
	if cfg.ContextWindow > 0 {
		pool.SetContextWindow(cfg.ContextWindow)
	}

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
	h.OnReadFile = func(path string) (string, error) {
		data, err := os.ReadFile(path)
		if err != nil {
			return "", err
		}
		return string(data), nil
	}
	h.OnWriteFile = func(path, content string) error {
		if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
			return err
		}
		return os.WriteFile(path, []byte(content), 0o600)
	}
	h.OnHTTPPost = httpPost
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

	// Register stateless tools as native Go functions — bypasses WASM entirely.
	registerNativeTools(h)

	// get_agent_status: returns turn count and recent history for a running agent.
	h.RegisterNativeTool(sdk.Tool{
		Name:        "get_agent_status",
		Description: "Get the status and recent conversation history of a running agent. Returns turn count and the last N messages.",
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
			"agent_id":     in.AgentID,
			"turn_count":   len(history) / 2,
			"last_summary": a.LastSummary(),
			"recent":       msgs,
		})
		return string(out), false
	})

	// Load built-in trusted WASM extensions.
	builtins := []struct {
		name string
		data []byte
	}{
		{"agents", agentsWASM},
		{"history", historyWASM},
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

// httpPost performs an HTTP POST request and returns the status code and body.
// Extracted from main() to keep cyclomatic complexity within the project limit.
func httpPost(url string, headers map[string]string, body []byte) (int, []byte, error) {
	req, err := http.NewRequest(http.MethodPost, url, bytes.NewReader(body))
	if err != nil {
		return 0, nil, err
	}
	for k, v := range headers {
		req.Header.Set(k, v)
	}
	resp, err := (&http.Client{Timeout: 5 * time.Second}).Do(req) //nolint:gosec // URL is from user config; SSRF is intentional
	if err != nil {
		return 0, nil, err
	}
	defer func() { _ = resp.Body.Close() }()
	var buf bytes.Buffer
	if _, err = buf.ReadFrom(resp.Body); err != nil {
		return resp.StatusCode, nil, err
	}
	return resp.StatusCode, buf.Bytes(), nil
}
