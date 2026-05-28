package main

import (
	"context"
	"encoding/json"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"strings"

	fantasy "charm.land/fantasy"
	fantasyanthropicprovider "charm.land/fantasy/providers/anthropic"
	fantasygoogleprovider "charm.land/fantasy/providers/google"
	fantasyopenapiprovider "charm.land/fantasy/providers/openai"
	"github.com/mattdurham/wllr/extension"
	"github.com/mattdurham/wllr/sdk"
)

// providerAnthropic is the canonical provider name for Anthropic.
const providerAnthropic = "anthropic"

// buildProvider constructs a fantasy.Provider and fetches the configured
// LanguageModel from it. Returns an error if the provider name is unknown
// or if the provider or model cannot be created.
func buildProvider(ctx context.Context, cfg *Config) (fantasy.Provider, fantasy.LanguageModel, error) {
	var (
		prov    fantasy.Provider
		provErr error
	)

	switch cfg.Provider {
	case providerAnthropic:
		anthropicOpts := []fantasyanthropicprovider.Option{
			fantasyanthropicprovider.WithAPIKey(cfg.AnthropicAPIKey),
		}
		// OAuth tokens (sk-ant-oat...) are Claude Code subscription tokens.
		// They route through a higher-limit tier and require Claude Code beta headers
		// to identify the client correctly — matching how pi/Claude Code authenticates.
		if strings.HasPrefix(cfg.AnthropicAPIKey, "sk-ant-oat") {
			anthropicOpts = append(anthropicOpts, fantasyanthropicprovider.WithHeaders(map[string]string{
				"anthropic-beta": "claude-code-20250219,oauth-2025-04-20",
				"user-agent":     "claude-cli/1.0.0",
				"x-app":          "cli",
			}))
		}
		prov, provErr = fantasyanthropicprovider.New(anthropicOpts...)
	case "openai":
		prov, provErr = fantasyopenapiprovider.New(
			fantasyopenapiprovider.WithAPIKey(cfg.OpenAIAPIKey),
		)
	case "gemini":
		prov, provErr = fantasygoogleprovider.New(
			fantasygoogleprovider.WithGeminiAPIKey(cfg.GeminiAPIKey),
		)
	default:
		return nil, nil, fmt.Errorf("unknown provider %q", cfg.Provider)
	}

	if provErr != nil {
		return nil, nil, fmt.Errorf("create provider: %w", provErr)
	}

	lm, err := prov.LanguageModel(ctx, cfg.Model)
	if err != nil {
		return nil, nil, fmt.Errorf("get language model %q from provider %q: %w", cfg.Model, cfg.Provider, err)
	}

	return prov, lm, nil
}

// registerNativeTools registers the four stateless native Go tools (read_file,
// write_file, exec, get_env) on h, bypassing WASM entirely.
func registerNativeTools(h *extension.Host) {
	h.RegisterNativeTool(sdk.Tool{
		Name:        "read_file",
		Description: "Read the contents of a file from the filesystem",
		InputSchema: json.RawMessage(`{"type":"object","properties":{"path":{"type":"string","description":"Absolute or relative path of the file to read"}},"required":["path"]}`),
	}, func(_ context.Context, input json.RawMessage) (string, bool) {
		var in struct {
			Path string `json:"path"`
		}
		if err := json.Unmarshal(input, &in); err != nil || in.Path == "" {
			return "path is required", true
		}
		content, err := os.ReadFile(in.Path)
		if err != nil {
			return "read_file: " + err.Error(), true
		}
		return string(content), false
	})

	h.RegisterNativeTool(sdk.Tool{
		Name:        "write_file",
		Description: "Write content to a file on the filesystem",
		InputSchema: json.RawMessage(`{"type":"object","properties":{"path":{"type":"string","description":"Path of the file to write"},"content":{"type":"string","description":"Content to write to the file"}},"required":["path","content"]}`),
	}, func(_ context.Context, input json.RawMessage) (string, bool) {
		var in struct {
			Path    string `json:"path"`
			Content string `json:"content"`
		}
		if err := json.Unmarshal(input, &in); err != nil || in.Path == "" {
			return "path is required", true
		}
		if err := os.MkdirAll(filepath.Dir(in.Path), 0o755); err != nil {
			return "write_file: " + err.Error(), true
		}
		if err := os.WriteFile(in.Path, []byte(in.Content), 0o600); err != nil {
			return "write_file: " + err.Error(), true
		}
		return fmt.Sprintf("written %d bytes to %s", len(in.Content), in.Path), false
	})

	h.RegisterNativeTool(sdk.Tool{
		Name:        "exec",
		Description: "Execute a shell command on the host system",
		InputSchema: json.RawMessage(`{"type":"object","properties":{"command":{"type":"string","description":"Shell command to execute"},"dir":{"type":"string","description":"Working directory (optional, defaults to current)"}},"required":["command"]}`),
	}, func(ctx context.Context, input json.RawMessage) (string, bool) {
		var in struct {
			Command string `json:"command"`
			Dir     string `json:"dir"`
		}
		if err := json.Unmarshal(input, &in); err != nil || in.Command == "" {
			return "command is required", true
		}
		cmd := exec.CommandContext(ctx, "sh", "-c", in.Command)
		if in.Dir != "" {
			cmd.Dir = in.Dir
		}
		out, err := cmd.CombinedOutput()
		output := string(out)
		if err != nil {
			if ctx.Err() != nil {
				return "exec cancelled", true
			}
			if output == "" {
				return err.Error(), true
			}
			return output + "\nerror: " + err.Error(), true
		}
		return output, false
	})

	h.RegisterNativeTool(sdk.Tool{
		Name:        "get_env",
		Description: "Read environment variables from the host system",
		InputSchema: json.RawMessage(`{"type":"object","properties":{"name":{"type":"string","description":"Specific env var name to look up (optional — omit to get all)"}}}`),
	}, func(_ context.Context, input json.RawMessage) (string, bool) {
		var in struct {
			Name string `json:"name"`
		}
		_ = json.Unmarshal(input, &in) // best-effort; empty in.Name means "return all"
		if in.Name != "" {
			return os.Getenv(in.Name), false
		}
		vars := os.Environ()
		data, _ := json.Marshal(vars)
		return string(data), false
	})
}
