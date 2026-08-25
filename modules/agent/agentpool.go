package agent

// NOTE: Any changes to this file must be reflected in the corresponding SPECS.md or NOTES.md.

import (
	"sync"
	"sync/atomic"

	"charm.land/fantasy"
	"github.com/mattdurham/wllr/modules/sdk"
)

// AgentPool manages all live agents and a shared token counter.
// It is safe for concurrent use from multiple goroutines.
type AgentPool struct {
	provider fantasy.Provider
	agents   map[string]*Agent
	teams    map[string]*Team
	// contextUsageDispatcher, when set, is called after each completed turn on any
	// agent so the harness can forward EventContextUsage to WASM extensions without
	// a circular import between the agent and extension packages.
	// compactions is the agent's cumulative successful-compaction count.
	// Set via SetContextUsageDispatcher; safe to call before any Submit.
	contextUsageDispatcher func(cu sdk.ContextUsage, compacted bool, thresholdPct float64, compactions int)
	// wakeNotifier, when set, is called with an agent ID whenever Deliver wakes
	// that agent (wake=true). The harness uses it to drive the TUI streaming
	// indicator for the main agent. Set via SetWakeNotifier.
	wakeNotifier func(id string)
	// providerRequestInterceptor, when set, runs the before_provider_request
	// transform chain just before each agent turn streams to the provider. It can
	// redact the outgoing messages, reroute the model, or block the request.
	// Installed by the harness (routes to extension DispatchEventChain) to avoid
	// an agent→extension circular import. Set via SetProviderRequestInterceptor.
	providerRequestInterceptor ProviderRequestInterceptor
	providerName               string
	defaultModelName           string
	baseSystemPrompt           string
	// compactConfig controls the percentage-based compaction trigger.
	// Initialized from WLLR_COMPACT_THRESHOLD in NewPool; override via SetCompactConfig.
	compactConfig      CompactConfig
	contextWindow      int64
	tokenCount         atomic.Int64
	mu                 sync.RWMutex
	baseSystemPromptMu sync.RWMutex
	dispatchMu         sync.RWMutex
}
