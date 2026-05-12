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
	"strings"
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

	// run_agent: synchronous sub-agent call. Spawns a sub-agent, waits for it
	// to complete, and returns its output inline as the tool result — keeping
	// the orchestrator in one turn so context is never lost.
	h.RegisterNativeTool(sdk.Tool{
		Name:        "run_agent",
		Description: "Run a sub-agent synchronously and return its output. Use this when you need a result before continuing. The agent runs to completion and its output is returned as this tool's result.",
		InputSchema: json.RawMessage(`{"type":"object","properties":{"name":{"type":"string","description":"Short label for the agent"},"system_prompt":{"type":"string","description":"Role and constraints for the agent"},"prompt":{"type":"string","description":"Task to execute"},"model":{"type":"string","description":"Model name (optional)"}},"required":["name","system_prompt","prompt"]}`),
	}, func(ctx context.Context, input json.RawMessage) (string, bool) {
		var in struct {
			Name         string `json:"name"`
			SystemPrompt string `json:"system_prompt"`
			Prompt       string `json:"prompt"`
			Model        string `json:"model"`
		}
		if err := json.Unmarshal(input, &in); err != nil || in.Prompt == "" {
			return "name, system_prompt, and prompt are required", true
		}
		lm, err := pool.LanguageModelForModel(ctx, in.Model)
		if err != nil {
			return "could not get language model: " + err.Error(), true
		}
		agentID := "run_agent_" + in.Name
		a, err := pool.Spawn(agentID, lm, agent.SpawnOpts{SystemPrompt: in.SystemPrompt})
		if err != nil {
			return "spawn failed: " + err.Error(), true
		}
		var collected strings.Builder
		done := make(chan error, 1)
		a.SetOnToken(func(tok string) { collected.WriteString(tok) })
		a.SetOnDone(func(e error) { done <- e })
		a.SetToolsFn(func() []fantasy.AgentTool {
			return harness.BuildFantasyTools(h, agentID, nil)
		})
		if err := pool.Send(agentID, in.Prompt); err != nil {
			_ = pool.Close(agentID)
			return "send failed: " + err.Error(), true
		}
		select {
		case err := <-done:
			_ = pool.Close(agentID)
			if err != nil {
				return fmt.Sprintf("agent error: %v", err), true
			}
			result := strings.TrimSpace(collected.String())
			if result == "" {
				return "(agent produced no output)", false
			}
			return result, false
		case <-ctx.Done():
			_ = pool.Close(agentID)
			return "agent timed out", true
		}
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
