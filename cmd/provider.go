package main

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"net/http"
	"os"
	"os/exec"
	"path/filepath"
	"sort"
	"strings"
	"sync"
	"syscall"
	"time"

	fantasy "charm.land/fantasy"
	fantasyanthropicprovider "charm.land/fantasy/providers/anthropic"
	fantasygoogleprovider "charm.land/fantasy/providers/google"
	fantasyopenapiprovider "charm.land/fantasy/providers/openai"
	fantasyopenrouterprovider "charm.land/fantasy/providers/openrouter"
	"github.com/mattdurham/wllr/modules/extension"
	"github.com/mattdurham/wllr/modules/sdk"
)

const (
	// providerAnthropic is the canonical provider name for Anthropic.
	providerAnthropic  = "anthropic"
	providerOpenAI     = "openai"
	providerGemini     = "gemini"
	providerOpenRouter = "openrouter"
	providerLocal      = "local"

	defaultAnthropicModel = "claude-sonnet-4-6"
	defaultOpenAIModel    = "gpt-5.5"

	defaultExecTimeout = 30 * time.Second
	execKillGrace      = time.Second
)

var (
	errExecCancelled = errors.New("exec cancelled")
	errExecTimedOut  = errors.New("exec timed out")
)

// newCodexProvider builds an OpenAI fantasy.Provider pointed at the ChatGPT
// Codex backend, authenticated with an OAuth access token (Bearer) and the
// ChatGPT account-id header. This is how a ChatGPT Plus/Pro subscription token
// is used instead of a standard OpenAI API key — matching how Codex/pi route
// these requests. The Responses API is used (Codex models are responses-based).
//
// WithResponsesAPIFunc overrides fantasy's default IsResponsesModel gate, whose
// exact-ID list and gpt-N regex only cover models OpenAI shipped when fantasy
// was released. Every model on the codex backend is served by the Responses
// API regardless of its ID (new codex models don't follow the gpt-N pattern),
// so routing is unconditional here: a future model ID needs no fantasy bump
// and no wllr change to reach the right endpoint.
func newCodexProvider(accessToken, accountID string) (fantasy.Provider, error) {
	return fantasyopenapiprovider.New(
		fantasyopenapiprovider.WithAPIKey(accessToken),
		fantasyopenapiprovider.WithBaseURL("https://chatgpt.com/backend-api/codex"),
		fantasyopenapiprovider.WithHeaders(map[string]string{
			"chatgpt-account-id": accountID,
			"OpenAI-Beta":        "responses=experimental",
			"originator":         "codex_cli_go",
		}),
		fantasyopenapiprovider.WithUseResponsesAPI(),
		fantasyopenapiprovider.WithResponsesAPIFunc(func(string) bool { return true }),
	)
}

// newAnthropicProvider builds an Anthropic fantasy.Provider for the given key.
// OAuth tokens (sk-ant-oat... access tokens) are Claude Code subscription
// tokens: they route through a higher-limit tier and require the Claude Code
// beta headers to identify the client correctly — matching how pi/Claude Code
// authenticate. Extracted so the OAuth login flow can rebuild the provider with
// a freshly obtained access token.
func newAnthropicProvider(apiKey string) (fantasy.Provider, error) {
	opts := []fantasyanthropicprovider.Option{
		fantasyanthropicprovider.WithAPIKey(apiKey),
	}
	if strings.HasPrefix(apiKey, "sk-ant-oat") {
		opts = append(opts, fantasyanthropicprovider.WithHeaders(map[string]string{
			"anthropic-beta": "claude-code-20250219,oauth-2025-04-20",
			"user-agent":     "claude-cli/1.0.0",
			"x-app":          "cli",
		}))
	}
	return fantasyanthropicprovider.New(opts...)
}

// subagentLanguageModel binds local sub-agents to the endpoint and credentials
// of their configured model. An explicit endpoint must match that entry.
func subagentLanguageModel(
	ctx context.Context,
	cfg *Config,
	currentProvider fantasy.Provider,
	model, endpoint string,
) (fantasy.LanguageModel, error) {
	if cfg.Provider != providerLocal {
		if endpoint != "" {
			return nil, fmt.Errorf("endpoint is only supported for configured local models")
		}
		return currentProvider.LanguageModel(ctx, model)
	}
	entry, ok := cfg.localModelByID(model)
	if !ok || entry.BaseURL == "" {
		return nil, fmt.Errorf("local model %q is not configured in wllr.local_models", model)
	}
	if endpoint != "" && endpoint != entry.BaseURL {
		return nil, fmt.Errorf("endpoint %q does not match configured endpoint for local model %q", endpoint, model)
	}
	provider, err := fantasyopenapiprovider.New(
		fantasyopenapiprovider.WithAPIKey(entry.APIKey),
		fantasyopenapiprovider.WithBaseURL(entry.BaseURL),
	)
	if err != nil {
		return nil, fmt.Errorf("create local model provider: %w", err)
	}
	return provider.LanguageModel(ctx, model)
}

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
		prov, provErr = newAnthropicProvider(cfg.AnthropicAPIKey)
	case providerOpenAI:
		// A stored Codex OAuth token routes through the ChatGPT backend (with the
		// account-id header); a plain API key uses the default OpenAI API.
		if cred, ok := loadAuthCredential(providerOpenAI); ok && cred.Type == authTypeOAuth && cred.Access != "" {
			prov, provErr = newCodexProvider(cred.Access, cred.AccountID)
		} else {
			prov, provErr = fantasyopenapiprovider.New(
				fantasyopenapiprovider.WithAPIKey(cfg.OpenAIAPIKey),
			)
		}
	case providerGemini:
		prov, provErr = fantasygoogleprovider.New(
			fantasygoogleprovider.WithGeminiAPIKey(cfg.GeminiAPIKey),
		)
	case providerOpenRouter:
		prov, provErr = fantasyopenrouterprovider.New(
			fantasyopenrouterprovider.WithAPIKey(cfg.OpenRouterAPIKey),
			fantasyopenrouterprovider.WithHTTPClient(openRouterRetryClient{base: http.DefaultClient}),
		)
	case providerLocal:
		if !cfg.applyLocalModelSelection(cfg.Model) {
			return nil, nil, fmt.Errorf("local provider requires wllr.local_models with a matching model")
		}
		if cfg.LocalBaseURL == "" {
			return nil, nil, fmt.Errorf("local provider requires wllr.local_models with a matching model")
		}
		if cfg.Model == "" {
			return nil, nil, fmt.Errorf("local provider requires wllr.model, WLLR_MODEL, or a configured local model")
		}
		prov, provErr = fantasyopenapiprovider.New(
			fantasyopenapiprovider.WithAPIKey(cfg.LocalAPIKey),
			fantasyopenapiprovider.WithBaseURL(cfg.LocalBaseURL),
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

// editFileInput represents the input schema for edit_file.
type editFileInput struct {
	Path  string `json:"path"`
	Edits []struct {
		OldText string `json:"oldText"`
		NewText string `json:"newText"`
	} `json:"edits"`
}

// editFileResult represents the structured output from edit_file.
type editFileResult struct {
	Message string   `json:"message"`
	Edits   []int    `json:"edits,omitempty"`
	Errors  []string `json:"errors,omitempty"`
	Success bool     `json:"success"`
}

// registerNativeTools registers the native Go tools (read_file, write_file,
// exec, get_env, edit_file) on h, bypassing WASM entirely.
func registerNativeTools(h *extension.Host) {
	h.RegisterNativeTool(sdk.Tool{
		Name: "read_file",
		Description: "Read the contents of a file from the filesystem. Optionally return only a " +
			"1-based inclusive line range (start_line/end_line). Output is byte-capped with an " +
			"explicit truncation notice; when a path does not exist, nearby candidate paths are suggested.",
		InputSchema: json.RawMessage(
			`{"type":"object","properties":{"path":{"type":"string","description":"Absolute or relative path of the file to read"},"start_line":{"type":"integer","description":"Optional 1-based first line to return (inclusive)"},"end_line":{"type":"integer","description":"Optional 1-based last line to return (inclusive)"},"max_bytes":{"type":"integer","description":"Optional maximum bytes to return before truncation (default 524288)"}},"required":["path"]}`,
		),
		OutputSchema: json.RawMessage(
			`{"type":"string","description":"File contents as text, with line-range selection and an explicit truncation notice when capped"}`,
		),
	}, func(_ context.Context, input json.RawMessage) (string, bool) {
		var in struct {
			Path      string `json:"path"`
			StartLine int    `json:"start_line"`
			EndLine   int    `json:"end_line"`
			MaxBytes  int    `json:"max_bytes"`
		}
		if err := json.Unmarshal(input, &in); err != nil || in.Path == "" {
			return "path is required", true
		}
		content, err := os.ReadFile(in.Path)
		if err != nil {
			return readFileError(in.Path, err), true
		}
		return formatReadFile(content, in.StartLine, in.EndLine, in.MaxBytes), false
	})

	h.RegisterNativeTool(sdk.Tool{
		Name:        "write_file",
		Description: "Write content to a file on the filesystem",
		InputSchema: json.RawMessage(
			`{"type":"object","properties":{"path":{"type":"string","description":"Path of the file to write"},"content":{"type":"string","description":"Content to write to the file"}},"required":["path","content"]}`,
		),
		OutputSchema: json.RawMessage(
			`{"type":"string","description":"Confirmation text: written <n> bytes to <path>"}`,
		),
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
		Name:        "edit_file",
		Description: "Perform exact text replacement edits to a file. Use this tool instead of sed, python, or other external tools for safe, atomic file editing. Every oldText must match exactly once; all edits are validated before any changes are applied.",
		InputSchema: json.RawMessage(
			`{"type":"object","properties":{"path":{"type":"string","description":"Path of the file to edit"},"edits":{"type":"array","description":"Array of {oldText, newText} pairs. Every oldText must match exactly once in the file.","items":{"type":"object","properties":{"oldText":{"type":"string","description":"Exact text to find"},"newText":{"type":"string","description":"Replacement text"}},"required":["oldText","newText"]}}},"required":["path","edits"]}`,
		),
		OutputSchema: json.RawMessage(
			`{"type":"object","properties":{"success":{"type":"boolean"},"message":{"type":"string"},"edits":{"type":"array","items":{"type":"integer"}},"errors":{"type":"array","items":{"type":"string"}}}}`,
		),
	}, func(_ context.Context, input json.RawMessage) (string, bool) {
		var in editFileInput
		if err := json.Unmarshal(input, &in); err != nil {
			return "edit_file: invalid input JSON", true
		}
		if in.Path == "" {
			return "edit_file: path is required", true
		}
		if len(in.Edits) == 0 {
			return "edit_file: edits array is required", true
		}

		// Read the file
		content, err := os.ReadFile(in.Path)
		if err != nil {
			return fmt.Sprintf("edit_file: cannot read file: %v", err), true
		}

		fileContent := string(content)

		// Validate all edits before making any changes
		type editMatch struct {
			start int
			end   int
			edit  int
		}
		var matches []editMatch

		for i, edit := range in.Edits {
			if edit.OldText == "" {
				result, _ := json.Marshal(editFileResult{
					Success: false,
					Message: "edit_file: oldText cannot be empty",
					Errors:  []string{fmt.Sprintf("edit[%d]: oldText is required", i)},
				})
				return string(result), false
			}

			// Find all occurrences of oldText
			occurrences := findOccurrences(fileContent, edit.OldText)

			if len(occurrences) == 0 {
				result, _ := json.Marshal(editFileResult{
					Success: false,
					Message: fmt.Sprintf("edit_file: no match for oldText at edit %d", i),
					Errors:  []string{fmt.Sprintf("edit[%d]: oldText not found: %q", i, edit.OldText)},
				})
				return string(result), false
			}

			if len(occurrences) > 1 {
				result, _ := json.Marshal(editFileResult{
					Success: false,
					Message: fmt.Sprintf("edit_file: ambiguous match at edit %d (%d occurrences)", i, len(occurrences)),
					Errors: []string{
						fmt.Sprintf(
							"edit[%d]: oldText found %d times, must match exactly once: %q",
							i,
							len(occurrences),
							edit.OldText,
						),
					},
				})
				return string(result), false
			}

			// Check for overlapping edits
			matchStart := occurrences[0]
			matchEnd := matchStart + len(edit.OldText)

			for _, existing := range matches {
				if overlaps(matchStart, matchEnd, existing.start, existing.end) {
					result, _ := json.Marshal(editFileResult{
						Success: false,
						Message: "edit_file: overlapping edits detected",
						Errors: []string{
							fmt.Sprintf("edit[%d] overlaps with edit[%d]", i, existing.edit),
						},
					})
					return string(result), false
				}
			}

			matches = append(matches, editMatch{
				start: matchStart,
				end:   matchEnd,
				edit:  i,
			})
		}

		// Sort matches by start position in reverse order for replacement
		sort.Slice(matches, func(i, j int) bool {
			return matches[i].start > matches[j].start
		})

		// Apply edits in reverse order to preserve positions
		newContent := fileContent
		for _, m := range matches {
			edit := in.Edits[m.edit]
			before := newContent[:m.start]
			after := newContent[m.end:]
			newContent = before + edit.NewText + after
		}

		// Write the file
		if err := os.WriteFile(in.Path, []byte(newContent), 0o600); err != nil {
			result, _ := json.Marshal(editFileResult{
				Success: false,
				Message: fmt.Sprintf("edit_file: failed to write file: %v", err),
			})
			return string(result), false
		}

		// Success
		editsApplied := make([]int, len(in.Edits))
		for i := range in.Edits {
			editsApplied[i] = i
		}
		result, _ := json.Marshal(editFileResult{
			Success: true,
			Message: fmt.Sprintf("edit_file: applied %d edit(s) to %s", len(in.Edits), in.Path),
			Edits:   editsApplied,
		})
		return string(result), false
	})

	h.RegisterNativeTool(sdk.Tool{
		Name:        "exec",
		Description: "Execute a shell command on the host system",
		InputSchema: json.RawMessage(
			`{"type":"object","properties":{"command":{"type":"string","description":"Shell command to execute"},"dir":{"type":"string","description":"Working directory (optional, defaults to current)"},"timeout_ms":{"type":"integer","description":"Optional timeout in milliseconds (defaults to 30000)"}},"required":["command"]}`,
		),
		OutputSchema: json.RawMessage(
			`{"type":"string","description":"Combined stdout/stderr text, or error text when execution fails"}`,
		),
	}, func(ctx context.Context, input json.RawMessage) (string, bool) {
		var in struct {
			Command   string `json:"command"`
			Dir       string `json:"dir"`
			TimeoutMS int    `json:"timeout_ms"`
		}
		if err := json.Unmarshal(input, &in); err != nil || in.Command == "" {
			return "command is required", true
		}
		timeout := defaultExecTimeout
		if in.TimeoutMS > 0 {
			timeout = time.Duration(in.TimeoutMS) * time.Millisecond
		}
		output, err := runShellCommand(ctx, in.Command, in.Dir, timeout)
		if err != nil {
			if errors.Is(err, errExecCancelled) {
				return "exec cancelled", true
			}
			if errors.Is(err, errExecTimedOut) {
				return fmt.Sprintf("exec timed out after %s", timeout), true
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
		InputSchema: json.RawMessage(
			`{"type":"object","properties":{"name":{"type":"string","description":"Specific env var name to look up (optional — omit to get all)"}}}`,
		),
		OutputSchema: json.RawMessage(
			`{"type":["string","array"],"description":"Environment variable value as text, or a JSON array of KEY=VALUE strings when name is omitted"}`,
		),
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

// defaultReadMaxBytes bounds a single read_file result so a very large file
// cannot blow the model's context window. A caller can raise it per call via
// max_bytes, and use start_line/end_line to page through a larger file.
const defaultReadMaxBytes = 512 * 1024

// readFileError formats a read_file failure. When the file is missing it
// appends nearby candidate paths so a caller can correct a wrong, misspelled,
// or relocated path without another blind guess.
func readFileError(path string, err error) string {
	msg := "read_file: " + err.Error()
	if !errors.Is(err, os.ErrNotExist) {
		return msg
	}
	candidates := suggestPaths(path)
	if len(candidates) == 0 {
		return msg
	}
	var b strings.Builder
	b.WriteString(msg)
	b.WriteString("\ndid you mean:")
	for _, c := range candidates {
		b.WriteString("\n  ")
		b.WriteString(c)
	}
	return b.String()
}

// formatReadFile renders a file's contents with an optional 1-based inclusive
// line range and a byte cap. Truncation is always surfaced explicitly so the
// caller knows the result is incomplete and how to page further.
func formatReadFile(content []byte, startLine, endLine, maxBytes int) string {
	text := string(content)

	if startLine > 0 || endLine > 0 {
		lines := strings.Split(text, "\n")
		n := len(lines)
		s := startLine
		if s < 1 {
			s = 1
		}
		if s > n {
			return fmt.Sprintf("[read_file: start_line %d is beyond end of file (%d lines)]", startLine, n)
		}
		e := endLine
		if e < 1 || e > n {
			e = n
		}
		if e < s {
			return fmt.Sprintf("[read_file: end_line %d is before start_line %d]", endLine, startLine)
		}
		text = strings.Join(lines[s-1:e], "\n")
	}

	if maxBytes <= 0 {
		maxBytes = defaultReadMaxBytes
	}
	total := len(text)
	if total > maxBytes {
		cut := maxBytes
		// Prefer a line boundary at or before the cap so a line is not cut mid-way.
		if idx := strings.LastIndexByte(text[:cut], '\n'); idx > 0 {
			cut = idx + 1
		}
		text = text[:cut] + fmt.Sprintf(
			"\n[read_file: truncated at %d bytes of %d total; use start_line/end_line to read more]",
			cut, total,
		)
	}
	return text
}

// maxPathSuggestions caps how many candidate paths a missing read reports.
const maxPathSuggestions = 5

// suggestPaths returns up to a few existing paths that may be the intended
// target of a missing read. It covers two failure modes seen in practice: a
// slightly misspelled basename in the same directory, and a file that lives at
// the same relative path under a sibling directory (for example a git worktree
// checkout beside the main repository). Work is bounded so a missing path in a
// large tree cannot turn one read into an expensive scan.
func suggestPaths(path string) []string {
	var out []string
	seen := map[string]bool{}
	add := func(p string) {
		if p == "" || seen[p] || len(out) >= maxPathSuggestions {
			return
		}
		if st, err := os.Stat(p); err == nil && !st.IsDir() {
			seen[p] = true
			out = append(out, p)
		}
	}
	for _, c := range suggestSameDir(filepath.Dir(path), filepath.Base(path)) {
		add(c)
	}
	if len(out) < maxPathSuggestions {
		abs, err := filepath.Abs(path)
		if err != nil {
			abs = path
		}
		for _, c := range suggestRelocated(abs) {
			add(c)
		}
	}
	return out
}

// suggestSameDir returns up to three file paths in parent whose names are
// similar to base (a typo or a near rename), closest first.
func suggestSameDir(parent, base string) []string {
	entries, err := os.ReadDir(parent)
	if err != nil {
		return nil
	}
	type scored struct {
		path string
		dist int
	}
	lb := strings.ToLower(base)
	var near []scored
	for _, e := range entries {
		if e.IsDir() {
			continue
		}
		name := strings.ToLower(e.Name())
		d := boundedEditDistance(name, lb, 2)
		if d <= 2 || strings.Contains(name, lb) || strings.Contains(lb, name) {
			near = append(near, scored{path: filepath.Join(parent, e.Name()), dist: d})
		}
	}
	sort.Slice(near, func(i, j int) bool { return near[i].dist < near[j].dist })
	var out []string
	for i, n := range near {
		if i >= 3 {
			break
		}
		out = append(out, n.path)
	}
	return out
}

// suggestRelocated returns candidate paths holding the same relative path as
// abs under a sibling directory of one of abs's existing ancestors. It walks up
// the ancestor chain so a missing path whose own parents do not exist (the
// common case) is still matched against an existing ancestor higher up.
func suggestRelocated(abs string) []string {
	var out []string
	dir := filepath.Dir(abs)
	for depth := 0; depth < 8 && len(out) < maxPathSuggestions; depth++ {
		parent := filepath.Dir(dir)
		if parent == dir || parent == "" {
			break
		}
		if st, err := os.Stat(dir); err == nil && st.IsDir() {
			out = append(out, relocatedUnder(dir, abs)...)
		}
		dir = parent
	}
	return out
}

// relocatedUnder varies the first path component of abs below each existing
// sibling of that component under dir, testing both the direct replacement and
// one wildcard level for roots that hold per-checkout subdirectories (for
// example replacing "repo" with "repo-worktrees/<name>").
func relocatedUnder(dir, abs string) []string {
	relFromDir, err := filepath.Rel(dir, abs)
	if err != nil || relFromDir == "." || strings.HasPrefix(relFromDir, "..") {
		return nil
	}
	comps := strings.Split(relFromDir, string(filepath.Separator))
	if len(comps) < 2 {
		return nil
	}
	first := comps[0]
	rest := filepath.Join(comps[1:]...)
	entries, err := os.ReadDir(dir)
	if err != nil {
		return nil
	}
	var out []string
	for _, e := range entries {
		if !e.IsDir() || strings.HasPrefix(e.Name(), ".") || e.Name() == first {
			continue
		}
		if len(out) >= maxPathSuggestions {
			break
		}
		sib := filepath.Join(dir, e.Name())
		out = append(out, filepath.Join(sib, rest))
		for _, sub := range childDirNames(sib) {
			if len(out) >= maxPathSuggestions {
				break
			}
			out = append(out, filepath.Join(sib, sub, rest))
		}
	}
	return out
}

// childDirNames returns the names of subdirectories of dir, excluding hidden
// entries. A read error yields no names.
func childDirNames(dir string) []string {
	entries, err := os.ReadDir(dir)
	if err != nil {
		return nil
	}
	var names []string
	for _, e := range entries {
		if e.IsDir() && !strings.HasPrefix(e.Name(), ".") {
			names = append(names, e.Name())
		}
	}
	return names
}

// boundedEditDistance returns the Levenshtein distance between a and b, capped
// at max. It returns max as soon as every path in a row already exceeds it, so
// clearly dissimilar strings cost little.
func boundedEditDistance(a, b string, max int) int {
	if a == b {
		return 0
	}
	la, lb := len(a), len(b)
	if la == 0 {
		return lb
	}
	if lb == 0 {
		return la
	}
	if la-lb > max || lb-la > max {
		return max
	}
	prev := make([]int, lb+1)
	cur := make([]int, lb+1)
	for j := 0; j <= lb; j++ {
		prev[j] = j
	}
	for i := 1; i <= la; i++ {
		cur[0] = i
		rowMin := cur[0]
		for j := 1; j <= lb; j++ {
			cost := 1
			if a[i-1] == b[j-1] {
				cost = 0
			}
			cur[j] = editMin3(prev[j]+1, cur[j-1]+1, prev[j-1]+cost)
			if cur[j] < rowMin {
				rowMin = cur[j]
			}
		}
		if rowMin > max {
			return max
		}
		prev, cur = cur, prev
	}
	if prev[lb] > max {
		return max
	}
	return prev[lb]
}

// editMin3 returns the smallest of three ints.
func editMin3(a, b, c int) int {
	m := a
	if b < m {
		m = b
	}
	if c < m {
		m = c
	}
	return m
}

// findOccurrences returns all start indices of substr in s.
func findOccurrences(s, substr string) []int {
	var occurrences []int
	start := 0
	for {
		pos := strings.Index(s[start:], substr)
		if pos == -1 {
			break
		}
		occurrences = append(occurrences, start+pos)
		start += pos + 1
	}
	return occurrences
}

// overlaps returns true if [aStart, aEnd) overlaps with [bStart, bEnd).
func overlaps(aStart, aEnd, bStart, bEnd int) bool {
	return aStart < bEnd && bStart < aEnd
}

type lockedBuffer struct {
	bytes.Buffer
	mu sync.Mutex
}

func (b *lockedBuffer) Write(p []byte) (int, error) {
	b.mu.Lock()
	defer b.mu.Unlock()
	return b.Buffer.Write(p)
}

func (b *lockedBuffer) String() string {
	b.mu.Lock()
	defer b.mu.Unlock()
	return b.Buffer.String()
}

func runShellCommand(ctx context.Context, command, dir string, timeout time.Duration) (string, error) {
	if timeout <= 0 {
		timeout = defaultExecTimeout
	}
	runCtx, cancel := context.WithTimeout(ctx, timeout)
	defer cancel()

	cmd := exec.Command("sh", "-c", command)
	cmd.SysProcAttr = &syscall.SysProcAttr{Setpgid: true}
	if dir != "" {
		cmd.Dir = dir
	}

	var output lockedBuffer
	cmd.Stdout = &output
	cmd.Stderr = &output

	if err := cmd.Start(); err != nil {
		return output.String(), err
	}

	done := make(chan error, 1)
	go func() {
		done <- cmd.Wait()
	}()

	select {
	case err := <-done:
		return output.String(), err
	case <-runCtx.Done():
		terminateProcessGroup(cmd.Process.Pid, syscall.SIGTERM)
		select {
		case <-done:
			return output.String(), execContextError(runCtx)
		case <-time.After(execKillGrace):
			terminateProcessGroup(cmd.Process.Pid, syscall.SIGKILL)
			<-done
			return output.String(), execContextError(runCtx)
		}
	}
}

func execContextError(ctx context.Context) error {
	if errors.Is(ctx.Err(), context.DeadlineExceeded) {
		return errExecTimedOut
	}
	return errExecCancelled
}

func terminateProcessGroup(pid int, sig syscall.Signal) {
	if pid <= 0 {
		return
	}
	if err := syscall.Kill(-pid, sig); err != nil && !errors.Is(err, syscall.ESRCH) {
		_ = syscall.Kill(pid, sig)
	}
}
