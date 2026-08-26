package main

import (
	"context"
	"fmt"
)

// NOTE: model catalog. Source: charmbracelet Catwalk (https://catwalk.charm.sh/v2/providers),
// the same model-metadata service used by crush. Regenerate by fetching that
// endpoint and updating the per-provider slices below. Context windows are the
// model's input window in tokens.
//
// This is intentionally core Go code (not a WASM extension): the model list is
// tied directly to the provider integrations in cmd/provider.go.

// thinkingMode represents a specific thinking mode supported by a model.
// Different models support different sets of thinking modes.
type thinkingMode struct {
	ID          string // Internal ID (e.g., "2048", "16384" for Anthropic; "low", "high" for OpenAI)
	Name        string // Human-readable name (e.g., "Low", "High")
	Description string // Optional description
}

// Standard OpenAI reasoning-effort mode IDs, shared by the openai provider
// (native) and the local provider (OpenAI-compatible endpoint). The IDs are
// the values sent as reasoning_effort. Local models may advertise a narrower,
// endpoint-specific set via discovery (see probeLocalReasoning).
const (
	thinkingModeNone    = "none"
	thinkingModeMinimal = "minimal"
	thinkingModeLow     = "low"
	thinkingModeMedium  = "medium"
	thinkingModeHigh    = "high"
	thinkingModeXHigh   = "xhigh"
)

// modelInfo describes one selectable model for the /model picker.
type modelInfo struct {
	ID           string
	Name         string
	LocalBaseURL string
	LocalAPIKey  string

	ContextWindow int64
	ThinkingModes []thinkingMode // Model-specific thinking modes

	// ReasoningDefault is the server-declared default reasoning setting for a
	// discovered local model (from the LM Studio app API). For a model whose
	// advertised modes already contain the standard "none" ID, an empty value
	// means the default is "none". Switching to a local model adopts this
	// default when no provider-specific thinking mode is saved.
	ReasoningDefault string

	// ReasoningDeclared reports whether the model's endpoint explicitly declared
	// its reasoning capability (LM Studio app API listing). When true and
	// ThinkingModes is empty, the model is known not to support reasoning and
	// the /thinking picker reports so instead of guessing.
	ReasoningDeclared bool
}

// modelCatalog maps a provider name (cfg.Provider) to its selectable models,
// most-capable first. Keys match the provider names accepted by buildProvider.
var modelCatalog = map[string][]modelInfo{
	providerAnthropic: {
		{ID: "claude-opus-4-8", Name: "Claude Opus 4.8", ContextWindow: 1000000, ThinkingModes: []thinkingMode{
			{ID: "2048", Name: "Low", Description: "Extended thinking (2K tokens)"},
			{ID: "4096", Name: "Medium-Low", Description: "Extended thinking (4K tokens)"},
			{ID: "16384", Name: "Medium", Description: "Extended thinking (16K tokens)"},
			{ID: "32768", Name: "High", Description: "Extended thinking (32K tokens)"},
			{ID: "65536", Name: "X-High", Description: "Extended thinking (64K tokens)"},
		}},
		{ID: "claude-opus-4-7", Name: "Claude Opus 4.7", ContextWindow: 1000000, ThinkingModes: []thinkingMode{
			{ID: "2048", Name: "Low", Description: "Extended thinking (2K tokens)"},
			{ID: "4096", Name: "Medium-Low", Description: "Extended thinking (4K tokens)"},
			{ID: "16384", Name: "Medium", Description: "Extended thinking (16K tokens)"},
			{ID: "32768", Name: "High", Description: "Extended thinking (32K tokens)"},
			{ID: "65536", Name: "X-High", Description: "Extended thinking (64K tokens)"},
		}},
		{ID: "claude-opus-4-6", Name: "Claude Opus 4.6", ContextWindow: 200000, ThinkingModes: []thinkingMode{
			{ID: "2048", Name: "Low", Description: "Extended thinking (2K tokens)"},
			{ID: "4096", Name: "Medium-Low", Description: "Extended thinking (4K tokens)"},
			{ID: "16384", Name: "Medium", Description: "Extended thinking (16K tokens)"},
			{ID: "32768", Name: "High", Description: "Extended thinking (32K tokens)"},
			{ID: "65536", Name: "X-High", Description: "Extended thinking (64K tokens)"},
		}},
		{ID: "claude-opus-4-5-20251101", Name: "Claude Opus 4.5", ContextWindow: 200000, ThinkingModes: []thinkingMode{
			{ID: "2048", Name: "Low", Description: "Extended thinking (2K tokens)"},
			{ID: "4096", Name: "Medium-Low", Description: "Extended thinking (4K tokens)"},
			{ID: "16384", Name: "Medium", Description: "Extended thinking (16K tokens)"},
			{ID: "32768", Name: "High", Description: "Extended thinking (32K tokens)"},
			{ID: "65536", Name: "X-High", Description: "Extended thinking (64K tokens)"},
		}},
		{ID: "claude-sonnet-5", Name: "Claude Sonnet 5", ContextWindow: 1000000, ThinkingModes: []thinkingMode{
			{ID: "2048", Name: "Low", Description: "Extended thinking (2K tokens)"},
			{ID: "4096", Name: "Medium-Low", Description: "Extended thinking (4K tokens)"},
			{ID: "16384", Name: "Medium", Description: "Extended thinking (16K tokens)"},
			{ID: "32768", Name: "High", Description: "Extended thinking (32K tokens)"},
			{ID: "65536", Name: "X-High", Description: "Extended thinking (64K tokens)"},
		}},
		{ID: defaultAnthropicModel, Name: "Claude Sonnet 4.6", ContextWindow: 200000, ThinkingModes: []thinkingMode{
			{ID: "2048", Name: "Low", Description: "Extended thinking (2K tokens)"},
			{ID: "4096", Name: "Medium-Low", Description: "Extended thinking (4K tokens)"},
			{ID: "16384", Name: "Medium", Description: "Extended thinking (16K tokens)"},
			{ID: "32768", Name: "High", Description: "Extended thinking (32K tokens)"},
			{ID: "65536", Name: "X-High", Description: "Extended thinking (64K tokens)"},
		}},
		{ID: "claude-sonnet-4-5-20250929", Name: "Claude Sonnet 4.5", ContextWindow: 200000, ThinkingModes: []thinkingMode{
			{ID: "2048", Name: "Low", Description: "Extended thinking (2K tokens)"},
			{ID: "4096", Name: "Medium-Low", Description: "Extended thinking (4K tokens)"},
			{ID: "16384", Name: "Medium", Description: "Extended thinking (16K tokens)"},
			{ID: "32768", Name: "High", Description: "Extended thinking (32K tokens)"},
			{ID: "65536", Name: "X-High", Description: "Extended thinking (64K tokens)"},
		}},
		{ID: "claude-haiku-4-5-20251001", Name: "Claude Haiku 4.5", ContextWindow: 200000, ThinkingModes: []thinkingMode{
			{ID: "none", Name: "None", Description: "No extended thinking"},
		}},
		{ID: "claude-opus-4-1-20250805", Name: "Claude Opus 4.1", ContextWindow: 200000, ThinkingModes: []thinkingMode{
			{ID: "2048", Name: "Low", Description: "Extended thinking (2K tokens)"},
			{ID: "4096", Name: "Medium-Low", Description: "Extended thinking (4K tokens)"},
			{ID: "16384", Name: "Medium", Description: "Extended thinking (16K tokens)"},
			{ID: "32768", Name: "High", Description: "Extended thinking (32K tokens)"},
			{ID: "65536", Name: "X-High", Description: "Extended thinking (64K tokens)"},
		}},
		{ID: "claude-opus-4-20250514", Name: "Claude Opus 4", ContextWindow: 200000, ThinkingModes: []thinkingMode{
			{ID: "2048", Name: "Low", Description: "Extended thinking (2K tokens)"},
			{ID: "4096", Name: "Medium-Low", Description: "Extended thinking (4K tokens)"},
			{ID: "16384", Name: "Medium", Description: "Extended thinking (16K tokens)"},
			{ID: "32768", Name: "High", Description: "Extended thinking (32K tokens)"},
			{ID: "65536", Name: "X-High", Description: "Extended thinking (64K tokens)"},
		}},
		{ID: "claude-sonnet-4-20250514", Name: "Claude Sonnet 4", ContextWindow: 200000, ThinkingModes: []thinkingMode{
			{ID: "2048", Name: "Low", Description: "Extended thinking (2K tokens)"},
			{ID: "4096", Name: "Medium-Low", Description: "Extended thinking (4K tokens)"},
			{ID: "16384", Name: "Medium", Description: "Extended thinking (16K tokens)"},
			{ID: "32768", Name: "High", Description: "Extended thinking (32K tokens)"},
			{ID: "65536", Name: "X-High", Description: "Extended thinking (64K tokens)"},
		}},
	},
	providerOpenAI: {
		{ID: defaultOpenAIModel, Name: "GPT-5.5", ContextWindow: 1050000, ThinkingModes: []thinkingMode{
			{ID: "none", Name: "None", Description: "No extended reasoning"},
			{ID: "minimal", Name: "Minimal", Description: "Minimal extended reasoning"},
			{ID: "low", Name: "Low", Description: "Extended reasoning (low effort)"},
			{ID: "medium", Name: "Medium", Description: "Extended reasoning (medium effort)"},
			{ID: "high", Name: "High", Description: "Extended reasoning (high effort)"},
			{ID: "xhigh", Name: "X-High", Description: "Extended reasoning (maximum effort)"},
		}},
		{ID: "gpt-5.5-pro", Name: "GPT-5.5 Pro", ContextWindow: 1050000, ThinkingModes: []thinkingMode{
			{ID: "none", Name: "None", Description: "No extended reasoning"},
			{ID: "minimal", Name: "Minimal", Description: "Minimal extended reasoning"},
			{ID: "low", Name: "Low", Description: "Extended reasoning (low effort)"},
			{ID: "medium", Name: "Medium", Description: "Extended reasoning (medium effort)"},
			{ID: "high", Name: "High", Description: "Extended reasoning (high effort)"},
			{ID: "xhigh", Name: "X-High", Description: "Extended reasoning (maximum effort)"},
		}},
		{ID: "gpt-5.4", Name: "GPT-5.4", ContextWindow: 1050000, ThinkingModes: []thinkingMode{
			{ID: "none", Name: "None", Description: "No extended reasoning"},
			{ID: "minimal", Name: "Minimal", Description: "Minimal extended reasoning"},
			{ID: "low", Name: "Low", Description: "Extended reasoning (low effort)"},
			{ID: "medium", Name: "Medium", Description: "Extended reasoning (medium effort)"},
			{ID: "high", Name: "High", Description: "Extended reasoning (high effort)"},
			{ID: "xhigh", Name: "X-High", Description: "Extended reasoning (maximum effort)"},
		}},
		{ID: "gpt-5.4-pro", Name: "GPT-5.4 Pro", ContextWindow: 1050000, ThinkingModes: []thinkingMode{
			{ID: "none", Name: "None", Description: "No extended reasoning"},
			{ID: "minimal", Name: "Minimal", Description: "Minimal extended reasoning"},
			{ID: "low", Name: "Low", Description: "Extended reasoning (low effort)"},
			{ID: "medium", Name: "Medium", Description: "Extended reasoning (medium effort)"},
			{ID: "high", Name: "High", Description: "Extended reasoning (high effort)"},
			{ID: "xhigh", Name: "X-High", Description: "Extended reasoning (maximum effort)"},
		}},
		{ID: "gpt-5.4-mini", Name: "GPT-5.4 Mini", ContextWindow: 400000, ThinkingModes: []thinkingMode{
			{ID: "none", Name: "None", Description: "No extended reasoning"},
			{ID: "minimal", Name: "Minimal", Description: "Minimal extended reasoning"},
			{ID: "low", Name: "Low", Description: "Extended reasoning (low effort)"},
			{ID: "medium", Name: "Medium", Description: "Extended reasoning (medium effort)"},
			{ID: "high", Name: "High", Description: "Extended reasoning (high effort)"},
			{ID: "xhigh", Name: "X-High", Description: "Extended reasoning (maximum effort)"},
		}},
		{ID: "gpt-5.4-nano", Name: "GPT-5.4 Nano", ContextWindow: 400000, ThinkingModes: []thinkingMode{
			{ID: "none", Name: "None", Description: "No extended reasoning"},
			{ID: "minimal", Name: "Minimal", Description: "Minimal extended reasoning"},
			{ID: "low", Name: "Low", Description: "Extended reasoning (low effort)"},
			{ID: "medium", Name: "Medium", Description: "Extended reasoning (medium effort)"},
			{ID: "high", Name: "High", Description: "Extended reasoning (high effort)"},
			{ID: "xhigh", Name: "X-High", Description: "Extended reasoning (maximum effort)"},
		}},
		{ID: "gpt-5.3-codex", Name: "GPT-5.3 Codex", ContextWindow: 400000, ThinkingModes: []thinkingMode{
			{ID: "none", Name: "None", Description: "No extended reasoning"},
			{ID: "minimal", Name: "Minimal", Description: "Minimal extended reasoning"},
			{ID: "low", Name: "Low", Description: "Extended reasoning (low effort)"},
			{ID: "medium", Name: "Medium", Description: "Extended reasoning (medium effort)"},
			{ID: "high", Name: "High", Description: "Extended reasoning (high effort)"},
			{ID: "xhigh", Name: "X-High", Description: "Extended reasoning (maximum effort)"},
		}},
		{ID: "gpt-5.2", Name: "GPT-5.2", ContextWindow: 400000, ThinkingModes: []thinkingMode{
			{ID: "none", Name: "None", Description: "No extended reasoning"},
			{ID: "minimal", Name: "Minimal", Description: "Minimal extended reasoning"},
			{ID: "low", Name: "Low", Description: "Extended reasoning (low effort)"},
			{ID: "medium", Name: "Medium", Description: "Extended reasoning (medium effort)"},
			{ID: "high", Name: "High", Description: "Extended reasoning (high effort)"},
			{ID: "xhigh", Name: "X-High", Description: "Extended reasoning (maximum effort)"},
		}},
		{ID: "gpt-5.2-codex", Name: "GPT-5.2 Codex", ContextWindow: 400000, ThinkingModes: []thinkingMode{
			{ID: "none", Name: "None", Description: "No extended reasoning"},
			{ID: "minimal", Name: "Minimal", Description: "Minimal extended reasoning"},
			{ID: "low", Name: "Low", Description: "Extended reasoning (low effort)"},
			{ID: "medium", Name: "Medium", Description: "Extended reasoning (medium effort)"},
			{ID: "high", Name: "High", Description: "Extended reasoning (high effort)"},
			{ID: "xhigh", Name: "X-High", Description: "Extended reasoning (maximum effort)"},
		}},
		{ID: "gpt-5.1", Name: "GPT-5.1", ContextWindow: 400000, ThinkingModes: []thinkingMode{
			{ID: "none", Name: "None", Description: "No extended reasoning"},
			{ID: "minimal", Name: "Minimal", Description: "Minimal extended reasoning"},
			{ID: "low", Name: "Low", Description: "Extended reasoning (low effort)"},
			{ID: "medium", Name: "Medium", Description: "Extended reasoning (medium effort)"},
			{ID: "high", Name: "High", Description: "Extended reasoning (high effort)"},
			{ID: "xhigh", Name: "X-High", Description: "Extended reasoning (maximum effort)"},
		}},
		{ID: "gpt-5.1-codex", Name: "GPT-5.1 Codex", ContextWindow: 400000, ThinkingModes: []thinkingMode{
			{ID: "none", Name: "None", Description: "No extended reasoning"},
			{ID: "minimal", Name: "Minimal", Description: "Minimal extended reasoning"},
			{ID: "low", Name: "Low", Description: "Extended reasoning (low effort)"},
			{ID: "medium", Name: "Medium", Description: "Extended reasoning (medium effort)"},
			{ID: "high", Name: "High", Description: "Extended reasoning (high effort)"},
			{ID: "xhigh", Name: "X-High", Description: "Extended reasoning (maximum effort)"},
		}},
		{ID: "gpt-5.1-codex-max", Name: "GPT-5.1 Codex Max", ContextWindow: 400000, ThinkingModes: []thinkingMode{
			{ID: "none", Name: "None", Description: "No extended reasoning"},
			{ID: "minimal", Name: "Minimal", Description: "Minimal extended reasoning"},
			{ID: "low", Name: "Low", Description: "Extended reasoning (low effort)"},
			{ID: "medium", Name: "Medium", Description: "Extended reasoning (medium effort)"},
			{ID: "high", Name: "High", Description: "Extended reasoning (high effort)"},
			{ID: "xhigh", Name: "X-High", Description: "Extended reasoning (maximum effort)"},
		}},
		{ID: "gpt-5.1-codex-mini", Name: "GPT-5.1 Codex Mini", ContextWindow: 400000, ThinkingModes: []thinkingMode{
			{ID: "none", Name: "None", Description: "No extended reasoning"},
			{ID: "minimal", Name: "Minimal", Description: "Minimal extended reasoning"},
			{ID: "low", Name: "Low", Description: "Extended reasoning (low effort)"},
			{ID: "medium", Name: "Medium", Description: "Extended reasoning (medium effort)"},
			{ID: "high", Name: "High", Description: "Extended reasoning (high effort)"},
			{ID: "xhigh", Name: "X-High", Description: "Extended reasoning (maximum effort)"},
		}},
		{ID: "gpt-5-codex", Name: "GPT-5 Codex", ContextWindow: 400000, ThinkingModes: []thinkingMode{
			{ID: "none", Name: "None", Description: "No extended reasoning"},
			{ID: "minimal", Name: "Minimal", Description: "Minimal extended reasoning"},
			{ID: "low", Name: "Low", Description: "Extended reasoning (low effort)"},
			{ID: "medium", Name: "Medium", Description: "Extended reasoning (medium effort)"},
			{ID: "high", Name: "High", Description: "Extended reasoning (high effort)"},
			{ID: "xhigh", Name: "X-High", Description: "Extended reasoning (maximum effort)"},
		}},
		{ID: "gpt-5", Name: "GPT-5", ContextWindow: 400000, ThinkingModes: []thinkingMode{
			{ID: "none", Name: "None", Description: "No extended reasoning"},
			{ID: "minimal", Name: "Minimal", Description: "Minimal extended reasoning"},
			{ID: "low", Name: "Low", Description: "Extended reasoning (low effort)"},
			{ID: "medium", Name: "Medium", Description: "Extended reasoning (medium effort)"},
			{ID: "high", Name: "High", Description: "Extended reasoning (high effort)"},
			{ID: "xhigh", Name: "X-High", Description: "Extended reasoning (maximum effort)"},
		}},
		{ID: "gpt-5-mini", Name: "GPT-5 Mini", ContextWindow: 400000, ThinkingModes: []thinkingMode{
			{ID: "none", Name: "None", Description: "No extended reasoning"},
			{ID: "minimal", Name: "Minimal", Description: "Minimal extended reasoning"},
			{ID: "low", Name: "Low", Description: "Extended reasoning (low effort)"},
			{ID: "medium", Name: "Medium", Description: "Extended reasoning (medium effort)"},
			{ID: "high", Name: "High", Description: "Extended reasoning (high effort)"},
			{ID: "xhigh", Name: "X-High", Description: "Extended reasoning (maximum effort)"},
		}},
		{ID: "gpt-5-nano", Name: "GPT-5 Nano", ContextWindow: 400000, ThinkingModes: []thinkingMode{
			{ID: "none", Name: "None", Description: "No extended reasoning"},
			{ID: "minimal", Name: "Minimal", Description: "Minimal extended reasoning"},
			{ID: "low", Name: "Low", Description: "Extended reasoning (low effort)"},
			{ID: "medium", Name: "Medium", Description: "Extended reasoning (medium effort)"},
			{ID: "high", Name: "High", Description: "Extended reasoning (high effort)"},
			{ID: "xhigh", Name: "X-High", Description: "Extended reasoning (maximum effort)"},
		}},
		{ID: "o4-mini", Name: "o4 Mini", ContextWindow: 200000, ThinkingModes: []thinkingMode{
			{ID: "none", Name: "None", Description: "No extended reasoning"},
			{ID: "minimal", Name: "Minimal", Description: "Minimal extended reasoning"},
			{ID: "low", Name: "Low", Description: "Extended reasoning (low effort)"},
			{ID: "medium", Name: "Medium", Description: "Extended reasoning (medium effort)"},
			{ID: "high", Name: "High", Description: "Extended reasoning (high effort)"},
			{ID: "xhigh", Name: "X-High", Description: "Extended reasoning (maximum effort)"},
		}},
		{ID: "o3", Name: "o3", ContextWindow: 200000, ThinkingModes: []thinkingMode{
			{ID: "none", Name: "None", Description: "No extended reasoning"},
			{ID: "minimal", Name: "Minimal", Description: "Minimal extended reasoning"},
			{ID: "low", Name: "Low", Description: "Extended reasoning (low effort)"},
			{ID: "medium", Name: "Medium", Description: "Extended reasoning (medium effort)"},
			{ID: "high", Name: "High", Description: "Extended reasoning (high effort)"},
			{ID: "xhigh", Name: "X-High", Description: "Extended reasoning (maximum effort)"},
		}},
		{ID: "gpt-4.1", Name: "GPT-4.1", ContextWindow: 1047576, ThinkingModes: []thinkingMode{
			{ID: "none", Name: "None", Description: "No extended reasoning"},
			{ID: "minimal", Name: "Minimal", Description: "Minimal extended reasoning"},
			{ID: "low", Name: "Low", Description: "Extended reasoning (low effort)"},
			{ID: "medium", Name: "Medium", Description: "Extended reasoning (medium effort)"},
			{ID: "high", Name: "High", Description: "Extended reasoning (high effort)"},
			{ID: "xhigh", Name: "X-High", Description: "Extended reasoning (maximum effort)"},
		}},
		{ID: "gpt-4.1-mini", Name: "GPT-4.1 Mini", ContextWindow: 1047576, ThinkingModes: []thinkingMode{
			{ID: "none", Name: "None", Description: "No extended reasoning"},
			{ID: "minimal", Name: "Minimal", Description: "Minimal extended reasoning"},
			{ID: "low", Name: "Low", Description: "Extended reasoning (low effort)"},
			{ID: "medium", Name: "Medium", Description: "Extended reasoning (medium effort)"},
			{ID: "high", Name: "High", Description: "Extended reasoning (high effort)"},
			{ID: "xhigh", Name: "X-High", Description: "Extended reasoning (maximum effort)"},
		}},
		{ID: "gpt-4.1-nano", Name: "GPT-4.1 Nano", ContextWindow: 1047576, ThinkingModes: []thinkingMode{
			{ID: "none", Name: "None", Description: "No extended reasoning"},
			{ID: "minimal", Name: "Minimal", Description: "Minimal extended reasoning"},
			{ID: "low", Name: "Low", Description: "Extended reasoning (low effort)"},
			{ID: "medium", Name: "Medium", Description: "Extended reasoning (medium effort)"},
			{ID: "high", Name: "High", Description: "Extended reasoning (high effort)"},
			{ID: "xhigh", Name: "X-High", Description: "Extended reasoning (maximum effort)"},
		}},
		{ID: "o3-mini", Name: "o3 Mini", ContextWindow: 200000, ThinkingModes: []thinkingMode{
			{ID: "none", Name: "None", Description: "No extended reasoning"},
			{ID: "minimal", Name: "Minimal", Description: "Minimal extended reasoning"},
			{ID: "low", Name: "Low", Description: "Extended reasoning (low effort)"},
			{ID: "medium", Name: "Medium", Description: "Extended reasoning (medium effort)"},
			{ID: "high", Name: "High", Description: "Extended reasoning (high effort)"},
			{ID: "xhigh", Name: "X-High", Description: "Extended reasoning (maximum effort)"},
		}},
		{ID: "gpt-4o", Name: "GPT-4o", ContextWindow: 128000, ThinkingModes: []thinkingMode{
			{ID: "none", Name: "None", Description: "No extended reasoning"},
			{ID: "minimal", Name: "Minimal", Description: "Minimal extended reasoning"},
			{ID: "low", Name: "Low", Description: "Extended reasoning (low effort)"},
			{ID: "medium", Name: "Medium", Description: "Extended reasoning (medium effort)"},
			{ID: "high", Name: "High", Description: "Extended reasoning (high effort)"},
			{ID: "xhigh", Name: "X-High", Description: "Extended reasoning (maximum effort)"},
		}},
		{ID: "gpt-4o-mini", Name: "GPT-4o-mini", ContextWindow: 128000, ThinkingModes: []thinkingMode{
			{ID: "none", Name: "None", Description: "No extended reasoning"},
			{ID: "minimal", Name: "Minimal", Description: "Minimal extended reasoning"},
			{ID: "low", Name: "Low", Description: "Extended reasoning (low effort)"},
			{ID: "medium", Name: "Medium", Description: "Extended reasoning (medium effort)"},
			{ID: "high", Name: "High", Description: "Extended reasoning (high effort)"},
			{ID: "xhigh", Name: "X-High", Description: "Extended reasoning (maximum effort)"},
		}},
	},
	providerGemini: {
		{ID: "gemini-3.5-flash", Name: "Gemini 3.5 Flash", ContextWindow: 1048576, ThinkingModes: []thinkingMode{
			{ID: "512", Name: "Minimal", Description: "Extended thinking (512 tokens)"},
			{ID: "4096", Name: "Low", Description: "Extended thinking (4K tokens)"},
			{ID: "16384", Name: "Medium", Description: "Extended thinking (16K tokens)"},
			{ID: "32768", Name: "High", Description: "Extended thinking (32K tokens)"},
			{ID: "65536", Name: "X-High", Description: "Extended thinking (64K tokens)"},
		}},
		{ID: "gemini-3.1-pro-preview-customtools", Name: "Gemini 3.1 Pro", ContextWindow: 1048576, ThinkingModes: []thinkingMode{
			{ID: "512", Name: "Minimal", Description: "Extended thinking (512 tokens)"},
			{ID: "4096", Name: "Low", Description: "Extended thinking (4K tokens)"},
			{ID: "16384", Name: "Medium", Description: "Extended thinking (16K tokens)"},
			{ID: "32768", Name: "High", Description: "Extended thinking (32K tokens)"},
			{ID: "65536", Name: "X-High", Description: "Extended thinking (64K tokens)"},
		}},
		{ID: "gemini-3-pro-preview", Name: "Gemini 3 Pro", ContextWindow: 1048576, ThinkingModes: []thinkingMode{
			{ID: "512", Name: "Minimal", Description: "Extended thinking (512 tokens)"},
			{ID: "4096", Name: "Low", Description: "Extended thinking (4K tokens)"},
			{ID: "16384", Name: "Medium", Description: "Extended thinking (16K tokens)"},
			{ID: "32768", Name: "High", Description: "Extended thinking (32K tokens)"},
			{ID: "65536", Name: "X-High", Description: "Extended thinking (64K tokens)"},
		}},
		{ID: "gemini-3-flash-preview", Name: "Gemini 3 Flash", ContextWindow: 1048576, ThinkingModes: []thinkingMode{
			{ID: "512", Name: "Minimal", Description: "Extended thinking (512 tokens)"},
			{ID: "4096", Name: "Low", Description: "Extended thinking (4K tokens)"},
			{ID: "16384", Name: "Medium", Description: "Extended thinking (16K tokens)"},
			{ID: "32768", Name: "High", Description: "Extended thinking (32K tokens)"},
			{ID: "65536", Name: "X-High", Description: "Extended thinking (64K tokens)"},
		}},
		{ID: "gemini-2.5-pro", Name: "Gemini 2.5 Pro", ContextWindow: 1048576, ThinkingModes: []thinkingMode{
			{ID: "512", Name: "Minimal", Description: "Extended thinking (512 tokens)"},
			{ID: "4096", Name: "Low", Description: "Extended thinking (4K tokens)"},
			{ID: "16384", Name: "Medium", Description: "Extended thinking (16K tokens)"},
			{ID: "32768", Name: "High", Description: "Extended thinking (32K tokens)"},
			{ID: "65536", Name: "X-High", Description: "Extended thinking (64K tokens)"},
		}},
		{ID: "gemini-2.5-flash", Name: "Gemini 2.5 Flash", ContextWindow: 1048576, ThinkingModes: []thinkingMode{
			{ID: "512", Name: "Minimal", Description: "Extended thinking (512 tokens)"},
			{ID: "4096", Name: "Low", Description: "Extended thinking (4K tokens)"},
			{ID: "16384", Name: "Medium", Description: "Extended thinking (16K tokens)"},
			{ID: "32768", Name: "High", Description: "Extended thinking (32K tokens)"},
			{ID: "65536", Name: "X-High", Description: "Extended thinking (64K tokens)"},
		}},
	},
}

var chatGPTOAuthModels = []modelInfo{
	{ID: defaultOpenAIModel, Name: "GPT-5.5", ContextWindow: 1050000, ThinkingModes: []thinkingMode{
		{ID: "none", Name: "None", Description: "No extended reasoning"},
		{ID: "minimal", Name: "Minimal", Description: "Minimal extended reasoning"},
		{ID: "low", Name: "Low", Description: "Extended reasoning (low effort)"},
		{ID: "medium", Name: "Medium", Description: "Extended reasoning (medium effort)"},
		{ID: "high", Name: "High", Description: "Extended reasoning (high effort)"},
		{ID: "xhigh", Name: "X-High", Description: "Extended reasoning (maximum effort)"},
	}},
	{ID: "gpt-5.4", Name: "GPT-5.4", ContextWindow: 1050000, ThinkingModes: []thinkingMode{
		{ID: "none", Name: "None", Description: "No extended reasoning"},
		{ID: "minimal", Name: "Minimal", Description: "Minimal extended reasoning"},
		{ID: "low", Name: "Low", Description: "Extended reasoning (low effort)"},
		{ID: "medium", Name: "Medium", Description: "Extended reasoning (medium effort)"},
		{ID: "high", Name: "High", Description: "Extended reasoning (high effort)"},
		{ID: "xhigh", Name: "X-High", Description: "Extended reasoning (maximum effort)"},
	}},
	{ID: "gpt-5.4-mini", Name: "GPT-5.4 Mini", ContextWindow: 400000, ThinkingModes: []thinkingMode{
		{ID: "none", Name: "None", Description: "No extended reasoning"},
		{ID: "minimal", Name: "Minimal", Description: "Minimal extended reasoning"},
		{ID: "low", Name: "Low", Description: "Extended reasoning (low effort)"},
		{ID: "medium", Name: "Medium", Description: "Extended reasoning (medium effort)"},
		{ID: "high", Name: "High", Description: "Extended reasoning (high effort)"},
		{ID: "xhigh", Name: "X-High", Description: "Extended reasoning (maximum effort)"},
	}},
}

var defaultModelsByProvider = map[string]string{
	providerAnthropic: defaultAnthropicModel,
	providerOpenAI:    defaultOpenAIModel,
	providerGemini:    "gemini-3-pro-preview",
}

// modelsForProvider returns the catalog for a provider, or nil if unknown.
func modelsForProvider(provider string) []modelInfo {
	return modelCatalog[provider]
}

func defaultModelForProvider(provider string) string {
	if model := defaultModelsByProvider[provider]; model != "" {
		return model
	}
	models := modelsForProvider(provider)
	if len(models) == 0 {
		return ""
	}
	return models[0].ID
}

func modelsForOpenAIAuth() []modelInfo {
	if cred, ok := loadAuthCredential(providerOpenAI); ok && cred.Type == authTypeOAuth {
		return chatGPTOAuthModels
	}
	return modelsForProvider(providerOpenAI)
}

func normalizeModelForProvider(provider, model string) string {
	if provider != providerOpenAI {
		return model
	}
	if cred, ok := loadAuthCredential(providerOpenAI); !ok || cred.Type != authTypeOAuth {
		return model
	}
	for _, m := range chatGPTOAuthModels {
		if m.ID == model {
			return model
		}
	}
	return defaultModelForProvider(provider)
}

// contextWindowFromCatalog returns the context window for a model ID within a
// provider, or 0 if not found.
func contextWindowFromCatalog(provider, id string) int64 {
	for _, m := range modelCatalog[provider] {
		if m.ID == id {
			return m.ContextWindow
		}
	}
	return 0
}

func contextWindowForSelection(provider, id string, cfg *Config) int64 {
	if cfg != nil && cfg.ContextWindowConfigured && cfg.ContextWindow > 0 {
		return cfg.ContextWindow
	}
	if provider == providerLocal && cfg != nil {
		// Explicit local_models config is authoritative over anything the
		// endpoint exposes; the pool-facing resolved window (what
		// rememberLocalModel applied) is the next source of truth.
		if lm, ok := cfg.localModelByID(id); ok && lm.ContextWindow > 0 {
			return lm.ContextWindow
		}
		if cfg.ContextWindow > 0 {
			return cfg.ContextWindow
		}
		return cfg.LocalContextWindow
	}
	return contextWindowFromCatalog(provider, id)
}

// supportedThinkingModesForModel returns the thinking modes supported by a
// given model. Precedence: the model catalog (openai and friends), explicit
// local_models configuration, and the standard OpenAI effort set for local
// models without an endpoint-declared set (OpenAI-compatible endpoints speak
// reasoning_effort).
func supportedThinkingModesForModel(provider, model string) []thinkingMode {
	if models := modelsForProvider(provider); models != nil {
		for _, m := range models {
			if m.ID == model {
				return m.ThinkingModes
			}
		}
	}
	if provider == providerLocal && model != "" {
		if lm, ok := loadWllrSettings().localModelEntry(model); ok && len(lm.ThinkingModes) > 0 {
			if filtered := thinkingModesFromIDs(lm.ThinkingModes); len(filtered) > 0 {
				return filtered
			}
		}
		return openAIStandardThinkingModes()
	}
	return nil
}

// openAIStandardThinkingModes is the full standard OpenAI reasoning-effort
// vocabulary, in picker order. Used for local models whose endpoint does not
// declare a supported set.
func openAIStandardThinkingModes() []thinkingMode {
	return []thinkingMode{
		{ID: thinkingModeNone, Name: "None", Description: "No extended reasoning"},
		{ID: thinkingModeMinimal, Name: "Minimal", Description: "Minimal extended reasoning"},
		{ID: thinkingModeLow, Name: "Low", Description: "Extended reasoning (low effort)"},
		{ID: thinkingModeMedium, Name: "Medium", Description: "Extended reasoning (medium effort)"},
		{ID: thinkingModeHigh, Name: "High", Description: "Extended reasoning (high effort)"},
		{ID: thinkingModeXHigh, Name: "X-High", Description: "Extended reasoning (maximum effort)"},
	}
}

// adoptedLocalThinkingMode returns the mode ID to adopt when switching to a
// local model with the given endpoint-declared default: the declared default
// itself if it is in the model's mode set, otherwise "none" if the set offers
// it, otherwise the first mode in the set.
func adoptedLocalThinkingMode(modes []thinkingMode, declaredDefault string) string {
	if len(modes) == 0 {
		return thinkingModeNone
	}
	if declaredDefault != "" {
		for _, m := range modes {
			if m.ID == declaredDefault {
				return m.ID
			}
		}
	}
	for _, m := range modes {
		if m.ID == thinkingModeNone {
			return m.ID
		}
	}
	return modes[0].ID
}

// thinkingModeInSet reports whether a mode ID is present in a mode set.
func thinkingModeInSet(modes []thinkingMode, id string) bool {
	for _, m := range modes {
		if m.ID == id {
			return true
		}
	}
	return false
}

// startupThinkingMode determines the thinking mode to apply for the current
// provider/model: the persisted mode when it is valid for that provider. For
// local models with an endpoint-declared capability set, a missing or stale
// persisted mode falls back to the model's endpoint-declared default (so the
// user sees and can adjust the server's own choice, and an invalid effort can
// never 400 the request). For non-local providers a stale/invalid mode yields
// "" (nothing applied — the legacy behavior).
func startupThinkingMode(ctx context.Context, cfg *Config, provider string) string {
	lvl := savedThinkingMode()
	if provider != providerLocal {
		if lvl == "" {
			return ""
		}
		if providerOptionsForThinkingMode(provider, lvl) != nil {
			return lvl
		}
		return ""
	}
	modes, declared, def := localThinkingInfo(ctx, cfg)
	if lvl != "" && thinkingModeInSet(modes, lvl) {
		return lvl
	}
	if declared {
		if len(modes) == 0 {
			// The endpoint says this model has no reasoning capability: send
			// nothing (it cannot think) rather than an effort it may reject.
			return ""
		}
		return adoptedLocalThinkingMode(modes, def)
	}
	// Endpoint did not declare capabilities (unknown OpenAI-compatible server):
	// trust only standard-vocabulary persisted modes; otherwise send nothing
	// (server default) rather than guessing.
	if lvl != "" {
		if _, ok := openAIReasoningEffortByMode[lvl]; ok {
			return lvl
		}
	}
	return ""
}

// currentThinkingModeForModel returns the currently active thinking mode ID for a model.
func currentThinkingModeForModel(provider, model string) string {
	// Get the saved thinking level from config
	savedLevel := savedThinkingLevel()
	if savedLevel == thinkingOff {
		return "none"
	}

	switch provider {
	case providerAnthropic:
		if budget, ok := anthropicThinkingBudget[savedLevel]; ok {
			return fmt.Sprint(budget)
		}
	case providerOpenAI:
		if effort, ok := openAIReasoningEffort[savedLevel]; ok {
			return string(effort)
		}
	case providerGemini:
		if budget, ok := geminiThinkingBudget[savedLevel]; ok {
			return fmt.Sprint(budget)
		}
	}

	return "none"
}
