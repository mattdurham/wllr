package agent

// NOTE: Any changes to this file must be reflected in the corresponding SPECS.md or NOTES.md.

import (
	"context"
	"sync"
	"sync/atomic"

	"charm.land/fantasy"
	"github.com/mattdurham/wllr/modules/sdk"
)

// AgentPool manages all live agents and a shared token counter.
// It is safe for concurrent use from multiple goroutines.
type AgentPool struct {
	provider     fantasy.Provider
	modelFactory ModelFactory
	// subagentResolver, when set, chooses the model for a sub-agent whose spawn
	// request omits an explicit model name. It returns the language model and
	// the resolved model name. This lets the host apply a configured working
	// model tier that may live on a different provider than the session model,
	// which the pool's own single-provider factory cannot express.
	// Set via SetSubagentResolver.
	subagentResolver func(ctx context.Context, requested string) (fantasy.LanguageModel, string, error)
	agents           map[string]*Agent
	// spawnSeq records the order agents were added to the pool, keyed by ID.
	// agents is a map, so iteration order is random; this preserves the order a
	// user expects to see (spawn order) for anything that lists agents.
	spawnSeq map[string]int64
	teams    map[string]*Team
	// contextUsageDispatcher, when set, is called after each completed turn on any
	// agent so the harness can forward EventContextUsage to WASM extensions without
	// a circular import between the agent and extension packages.
	// compactions is the agent's cumulative successful-compaction count.
	// Set via SetContextUsageDispatcher; safe to call before any Submit.
	contextUsageDispatcher func(cu sdk.ContextUsage, notice *CompactionNotice, compactions int)
	// wakeNotifier, when set, is called with an agent ID whenever Deliver wakes
	// that agent (wake=true). The harness uses it to drive the TUI streaming
	// indicator for the main agent. Set via SetWakeNotifier.
	wakeNotifier func(id string)
	// contextWindowObserver, when set, is called when a provider context-too-long
	// rejection reveals a model's real context window and the agent clamps its
	// session window to it. The host uses it to keep any window cache (the local
	// model picker and tier resolution) coherent with the correction without
	// giving the agent package a cmd dependency. previous is the window that was
	// replaced; authoritative is the provider-reported value now in effect.
	// Set via SetContextWindowObserver.
	contextWindowObserver func(model string, previous, authoritative int64)
	// providerRequestInterceptor, when set, runs the before_provider_request
	// transform chain just before each agent turn streams to the provider. It can
	// redact the outgoing messages, reroute the model, or block the request.
	// Installed by the harness (routes to extension DispatchEventChain) to avoid
	// an agent→extension circular import. Set via SetProviderRequestInterceptor.
	providerRequestInterceptor ProviderRequestInterceptor
	contextWindows             map[string]int64
	// toolOwnersFn resolves tool name to the extension that registered it for
	// the /context breakdown's per-extension tool attribution. Installed by the
	// harness from the extension host's RegisteredTools snapshot.
	toolOwnersFn     func() map[string]string
	providerName     string
	defaultModelName string
	baseSystemPrompt string
	// usageObservers are called once per completed agent turn with that turn's
	// token usage. The harness installs one to record per-model/per-agent
	// metrics without giving the agent package a metrics dependency; the API
	// server installs another to fan turn usage out to connected clients.
	// Set via SetUsageObserver (replace all) or AddUsageObserver (append).
	usageObservers []func(TurnUsage)
	// lifecycleObservers are called whenever an agent is added to or removed
	// from the pool. Set via SetLifecycleObserver (replace all) or
	// AddLifecycleObserver (append).
	lifecycleObservers []func(AgentLifecycle)
	// promptComponents labels the base system prompt's sources for the /context
	// breakdown. SetBaseSystemPrompt seeds a single fallback component; the
	// prompt extension replaces it with its internal decomposition via
	// set_system_prompt_components, and every labeled append adds an entry.
	// Display-only: nothing rebuilds the prompt from this list.
	promptComponents []sdk.SystemPromptComponent
	// compactConfig controls the percentage-based compaction trigger.
	// Initialized from WLLR_COMPACT_THRESHOLD in NewPool; override via SetCompactConfig.
	compactConfig      CompactConfig
	nextSeq            int64
	contextWindow      int64
	tokenCount         atomic.Int64
	mu                 sync.RWMutex
	baseSystemPromptMu sync.RWMutex
	dispatchMu         sync.RWMutex
}
