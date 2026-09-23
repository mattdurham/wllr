// Package agent manages sub-agents and teams for the bob harness.
// Each Agent wraps a fantasy.LanguageModel run loop with a message inbox.
// AgentPool owns all live agents and a shared token counter.
package agent

// NOTE: Any changes to this file must be reflected in the corresponding SPECS.md or NOTES.md.

import (
	"context"
	"errors"
	"fmt"
	"log/slog"
	"os"
	"sort"
	"strconv"
	"strings"

	"charm.land/fantasy"
	"github.com/mattdurham/wllr/modules/sdk"
)

// ModelFactory creates a language model for a model name and optional endpoint.
// The provider is the pool's current provider; endpoint is empty when the
// configured endpoint should be selected by the factory.
type ModelFactory func(ctx context.Context, provider fantasy.Provider, model, endpoint string) (fantasy.LanguageModel, error)

// MainAgentID is the conventional ID of the primary agent the user interacts with.
// All sub-agents derive their IDs from this (e.g. "main/coder", "main/team/worker").
const MainAgentID = "main"

var (
	// ErrAgentExists is returned when an agent with the same ID is spawned twice.
	ErrAgentExists = errors.New("agent: ID already exists")
	// ErrAgentNotFound is returned when an operation targets an unknown agent ID.
	ErrAgentNotFound = errors.New("agent: ID not found")
	// ErrTeamExists is returned when a team with the same ID is created twice.
	ErrTeamExists = errors.New("agent: team ID already exists")
	// ErrTeamNotFound is returned when an operation targets an unknown team ID.
	ErrTeamNotFound = errors.New("agent: team ID not found")
)

// AgentPool manages all live agents and a shared token counter.
// It is safe for concurrent use from multiple goroutines.

// provider is stored so sub-agents can request new language model instances
// for arbitrary model names.

// providerName is a human-readable display name for the provider (e.g. "anthropic").
// Set via SetProviderName; read via ProviderName.

// defaultModelName is used when LanguageModelForModel is called with an
// empty name (e.g. sub-agents that don't specify a model).

// contextWindow is retained as the compatibility/default-model value. Effective
// windows are stored per model in contextWindows so agents using different
// models do not share a compaction limit.

// tokenCount accumulates all text tokens emitted by all agents in this pool.

// baseSystemPrompt is accumulated from set_system_prompt / append_system_prompt
// and applied to every agent (current and future) so all agents share the
// same base context (AGENTS.md, skill list, etc.).

// NewPool creates an empty AgentPool. It reads WLLR_COMPACT_THRESHOLD from the
// environment to configure the percentage-based compaction trigger (default 0.80).
// Values > 1 are treated as percentages and divided by 100 (e.g. "90" → 0.90).
// Unparseable or empty values fall back to 0.80.
func NewPool() *AgentPool {
	threshold := 0.80
	if v := os.Getenv("WLLR_COMPACT_THRESHOLD"); v != "" {
		if parsed, err := strconv.ParseFloat(v, 64); err == nil && parsed > 0 {
			if parsed > 1 {
				parsed /= 100
			}
			if parsed > 1 {
				slog.Warn("WLLR_COMPACT_THRESHOLD value out of range, using default 0.80", "value", v)
				parsed = 0.80
			}
			threshold = parsed
		}
	}
	return &AgentPool{
		agents:         make(map[string]*Agent),
		spawnSeq:       make(map[string]int64),
		teams:          make(map[string]*Team),
		contextWindows: make(map[string]int64),
		compactConfig: CompactConfig{
			Enabled:      true,
			ThresholdPct: threshold,
		},
	}
}

// CompactConfig returns the current compaction configuration.
// Thread-safe.
func (p *AgentPool) CompactConfig() CompactConfig {
	p.mu.RLock()
	cfg := p.compactConfig
	p.mu.RUnlock()
	return cfg
}

// SetCompactConfig replaces the pool's compaction configuration.
// Thread-safe; may be called before or after agents are spawned.
func (p *AgentPool) SetCompactConfig(cfg CompactConfig) {
	p.mu.Lock()
	p.compactConfig = cfg
	p.mu.Unlock()
}

// SetContextUsageDispatcher installs a callback that is invoked after each completed
// agent turn with the current context window usage. Use this to forward
// EventContextUsage to WASM extensions from the harness layer without creating
// a circular import between the agent and extension packages.
// Thread-safe; may be called before or after agents are spawned.
func (p *AgentPool) SetContextUsageDispatcher(
	fn func(cu sdk.ContextUsage, compact bool, thresholdPct float64, compactions int),
) {
	p.dispatchMu.Lock()
	p.contextUsageDispatcher = fn
	p.dispatchMu.Unlock()
}

// dispatchContextUsage calls the registered contextUsageDispatcher, if any.
// Called from agent goroutines after each completed turn.
// compactions is the dispatching agent's cumulative successful-compaction
// count (additive observability data for the EventContextUsage payload).
func (p *AgentPool) dispatchContextUsage(cu sdk.ContextUsage, compacted bool, compactions int) {
	p.dispatchMu.RLock()
	fn := p.contextUsageDispatcher
	p.dispatchMu.RUnlock()
	if fn != nil {
		cfg := p.CompactConfig()
		fn(cu, compacted, cfg.ThresholdPct, compactions)
	}
}

// SetProviderRequestInterceptor installs the before_provider_request transform
// chain hook. The harness wires this to the extension host so interceptors can
// redact/reroute/block outgoing provider requests without an agent→extension
// circular import. Thread-safe; may be called before or after agents are spawned.
func (p *AgentPool) SetProviderRequestInterceptor(fn ProviderRequestInterceptor) {
	p.dispatchMu.Lock()
	p.providerRequestInterceptor = fn
	p.dispatchMu.Unlock()
}

// interceptProviderRequest runs the registered provider-request interceptor, if
// any. Returns the (possibly transformed) messages and model, whether the
// request is blocked, and the reason. With no interceptor it returns the inputs
// unchanged.
func (p *AgentPool) interceptProviderRequest(
	agentID string,
	messages []sdk.Message,
	model string,
) ([]sdk.Message, string, bool, string) {
	p.dispatchMu.RLock()
	fn := p.providerRequestInterceptor
	p.dispatchMu.RUnlock()
	if fn == nil {
		return messages, model, false, ""
	}
	return fn(agentID, messages, model)
}

// hasProviderRequestInterceptor reports whether a before_provider_request
// interceptor is installed. The agent uses this to keep the default turn path
// byte-identical when no interceptor exists (the transform path folds content
// into the message list and is only taken when interception is active).
func (p *AgentPool) hasProviderRequestInterceptor() bool {
	p.dispatchMu.RLock()
	ok := p.providerRequestInterceptor != nil
	p.dispatchMu.RUnlock()
	return ok
}

// SetWakeNotifier installs a callback invoked with an agent ID whenever Deliver
// wakes that agent (wake=true and the agent was idle). The harness uses it to
// drive the TUI streaming indicator for the main agent. Thread-safe.
func (p *AgentPool) SetWakeNotifier(fn func(id string)) {
	p.dispatchMu.Lock()
	p.wakeNotifier = fn
	p.dispatchMu.Unlock()
}

// notifyWake calls the registered wakeNotifier, if any.
func (p *AgentPool) notifyWake(id string) {
	p.dispatchMu.RLock()
	fn := p.wakeNotifier
	p.dispatchMu.RUnlock()
	if fn != nil {
		fn(id)
	}
}

// Deliver appends msg to the named agent's inbox and, when wake is true,
// ensures the agent processes it: if the agent is idle it starts a drain turn
// (empty-content Submit), and if it is already running the drain-until-empty
// pattern picks the message up when the current turn finishes. This is the
// single atomic "deliver and make sure it gets processed" primitive — it
// replaces the SendMessage+Send/Run two-call pattern at the call sites.
//
// Returns ErrAgentNotFound if id is unknown, or an error if msg.Content is empty.
// Non-blocking: any turn runs in a goroutine.
func (p *AgentPool) Deliver(id string, msg sdk.Message, wake bool) error {
	if strings.TrimSpace(msg.Content) == "" {
		return fmt.Errorf("Deliver: content must be non-empty (would cause API rejection)")
	}
	p.mu.RLock()
	a, exists := p.agents[id]
	p.mu.RUnlock()
	if !exists {
		return ErrAgentNotFound
	}
	a.AppendInbox(msg)
	if !wake {
		return nil
	}
	p.notifyWake(id)
	// Submit with empty content: the just-appended inbox message becomes the
	// turn's content via the drain path. If the agent is already running, the
	// CAS in Submit re-queues and the running turn's finishTurn drains it.
	a.Submit(context.Background(), "")
	return nil
}

// SetProvider stores the fantasy provider so OnAgentSpawn wiring in cmd/main.go
// can call LanguageModelForModel to create sub-agent language models.
func (p *AgentPool) SetProvider(prov fantasy.Provider) {
	p.provider = prov
}

// SetContextWindow sets the input context window for the current default model.
// New code should prefer SetModelContextWindow when the model is explicit.
func (p *AgentPool) SetContextWindow(tokens int64) {
	p.mu.Lock()
	p.contextWindow = tokens
	if p.contextWindows == nil {
		p.contextWindows = make(map[string]int64)
	}
	if p.defaultModelName != "" && tokens > 0 {
		p.contextWindows[strings.ToLower(p.defaultModelName)] = tokens
	}
	p.mu.Unlock()
}

// SetModelContextWindow records a resolved input context window for one model.
// A non-positive value removes the model's explicit metadata.
func (p *AgentPool) SetModelContextWindow(model string, tokens int64) {
	p.mu.Lock()
	defer p.mu.Unlock()
	if p.contextWindows == nil {
		p.contextWindows = make(map[string]int64)
	}
	model = strings.ToLower(strings.TrimSpace(model))
	if model == "" {
		return
	}
	if tokens > 0 {
		p.contextWindows[model] = tokens
	} else {
		delete(p.contextWindows, model)
	}
	if model == strings.ToLower(p.defaultModelName) {
		p.contextWindow = tokens
	}
}

// ContextWindowForModel returns the explicitly resolved window for model, or 0
// when the model still requires resolution.
func (p *AgentPool) ContextWindowForModel(model string) int64 {
	p.mu.RLock()
	window := p.contextWindows[strings.ToLower(strings.TrimSpace(model))]
	p.mu.RUnlock()
	if window > 0 {
		return window
	}
	return contextWindowForModel(model)
}

// ContextWindow returns the configured context window, or 0 if unset.
func (p *AgentPool) ContextWindow() int64 {
	p.mu.RLock()
	defer p.mu.RUnlock()
	return p.contextWindow
}

// MainAgentContextUsage returns the context window usage for the main agent.
// If the main agent has not completed a turn yet, all fields are zero.
// If no context window has been configured on the pool, ContextWindow and Percent
// will be zero.
func (p *AgentPool) MainAgentContextUsage() sdk.ContextUsage {
	p.mu.RLock()
	a := p.agents[MainAgentID]
	window := p.contextWindow
	p.mu.RUnlock()
	if a == nil {
		return sdk.ContextUsage{}
	}
	return sdk.ContextUsageFromFantasy(a.LastUsage(), window)
}

// SnapshotInbox returns a copy of an agent's inbox without draining. Snapshots
// are safe while the agent is running and are intended for status/UI views.
func (p *AgentPool) SnapshotInbox(id string) ([]sdk.Message, error) {
	a := p.Get(id)
	if a == nil {
		return nil, ErrAgentNotFound
	}
	return a.SnapshotInbox(), nil
}

// DeleteFromInbox removes message(s) from an agent's inbox.
func (p *AgentPool) DeleteFromInbox(id string, byIndex int, byMessageID string) (int, error) {
	a := p.Get(id)
	if a == nil {
		return 0, ErrAgentNotFound
	}
	if a.IsRunning() {
		return 0, errors.New("cannot modify inbox while agent is running")
	}
	return a.DeleteFromInbox(byIndex, byMessageID)
}

// EditInboxMessage updates a message's content.
func (p *AgentPool) EditInboxMessage(id string, byIndex int, byMessageID string, newContent string) error {
	a := p.Get(id)
	if a == nil {
		return ErrAgentNotFound
	}
	if a.IsRunning() {
		return errors.New("cannot modify inbox while agent is running")
	}
	return a.EditInboxMessage(byIndex, byMessageID, newContent)
}

// SetBaseSystemPrompt replaces the base system prompt and applies it to all
// current and future agents. Used by the context extension (AGENTS.md).
func (p *AgentPool) SetBaseSystemPrompt(prompt string) {
	p.baseSystemPromptMu.Lock()
	p.baseSystemPrompt = prompt
	p.baseSystemPromptMu.Unlock()
	p.mu.RLock()
	for _, a := range p.agents {
		a.SetSystemPrompt(prompt)
	}
	p.mu.RUnlock()
}

// AppendBaseSystemPrompt appends to the base system prompt and applies the
// addition to all current and future agents. Used by the skills extension.
func (p *AgentPool) AppendBaseSystemPrompt(text string) {
	p.baseSystemPromptMu.Lock()
	if p.baseSystemPrompt == "" {
		p.baseSystemPrompt = text
	} else {
		p.baseSystemPrompt += "\n\n" + text
	}
	p.baseSystemPromptMu.Unlock()
	p.mu.RLock()
	for _, a := range p.agents {
		a.AppendSystemPrompt(text)
	}
	p.mu.RUnlock()
}

// BaseSystemPrompt returns the accumulated base system prompt.
func (p *AgentPool) BaseSystemPrompt() string {
	p.baseSystemPromptMu.RLock()
	defer p.baseSystemPromptMu.RUnlock()
	return p.baseSystemPrompt
}

// SetDefaultModelName sets the model name used when spawning sub-agents that
// don't specify one.
func (p *AgentPool) SetDefaultModelName(name string) {
	p.mu.Lock()
	p.defaultModelName = name
	p.contextWindow = p.contextWindows[strings.ToLower(name)]
	p.mu.Unlock()
}

// DefaultModelName returns the pool's model used when a spawn request omits one.
func (p *AgentPool) DefaultModelName() string {
	p.mu.RLock()
	defer p.mu.RUnlock()
	return p.defaultModelName
}

// SetProviderName stores a human-readable display name for the configured provider.
// This is used by the harness status bar to show the active provider.
func (p *AgentPool) SetProviderName(name string) {
	p.mu.Lock()
	p.providerName = name
	p.mu.Unlock()
}

// ProviderName returns the display name set via SetProviderName.
func (p *AgentPool) ProviderName() string {
	p.mu.RLock()
	defer p.mu.RUnlock()
	return p.providerName
}

// LanguageModelForModel creates a language model for the named model using the
// pool's stored provider. Returns an error if no provider has been set or if
// the provider cannot satisfy the request.
func (p *AgentPool) LanguageModelForModel(ctx context.Context, model string) (fantasy.LanguageModel, error) {
	return p.LanguageModelForModelAtEndpoint(ctx, model, "")
}

// SetModelFactory installs the resolver used for sub-agent model requests.
// A nil factory restores the pool's default provider path.
func (p *AgentPool) SetModelFactory(factory ModelFactory) {
	p.mu.Lock()
	p.modelFactory = factory
	p.mu.Unlock()
}

// LanguageModelForModelAtEndpoint resolves a model using an optional endpoint.
// The configured factory, when present, selects how an empty endpoint is handled.
func (p *AgentPool) LanguageModelForModelAtEndpoint(
	ctx context.Context,
	model, endpoint string,
) (fantasy.LanguageModel, error) {
	p.mu.RLock()
	provider := p.provider
	factory := p.modelFactory
	defaultModel := p.defaultModelName
	p.mu.RUnlock()
	if model == "" {
		model = defaultModel
	}
	if provider == nil {
		return nil, errors.New("agent: no provider configured on pool")
	}
	if factory != nil {
		return factory(ctx, provider, model, endpoint)
	}
	if endpoint != "" {
		return nil, errors.New("agent: endpoint-specific model factory is not configured")
	}
	return provider.LanguageModel(ctx, model)
}

// EnsureMainAgent recreates the primary agent when a fatal model failure has
// removed it from the pool. It is intentionally limited to the primary agent;
// sub-agent lifecycle is owned by the orchestrator that created it.
func (p *AgentPool) EnsureMainAgent(ctx context.Context) error {
	if p.Get(MainAgentID) != nil {
		return nil
	}
	lm, err := p.LanguageModelForModel(ctx, "")
	if err != nil {
		return fmt.Errorf("create main agent model: %w", err)
	}
	if _, err := p.Spawn(MainAgentID, lm, SpawnOpts{TurnTimeout: -1}); err != nil && !errors.Is(err, ErrAgentExists) {
		return fmt.Errorf("spawn main agent: %w", err)
	}
	return nil
}

// Spawn creates and registers a new Agent with the given ID.
// Returns ErrAgentExists if the ID is already in use.
// The agent is not started; call agent.Submit to run its first turn.
func (p *AgentPool) Spawn(id string, lm fantasy.LanguageModel, opts SpawnOpts) (*Agent, error) {
	// The lifecycle report must run after the pool mutex is released, so the
	// unlock is explicit rather than deferred.
	p.mu.Lock()
	if _, exists := p.agents[id]; exists {
		p.mu.Unlock()
		return nil, ErrAgentExists
	}
	modelName := p.defaultModelName
	if opts.ModelName != "" {
		modelName = opts.ModelName
	}
	contextWindow := opts.ContextWindow
	if contextWindow <= 0 {
		contextWindow = p.contextWindows[strings.ToLower(modelName)]
	}
	if contextWindow <= 0 {
		contextWindow = contextWindowForModel(modelName)
	}
	if contextWindow <= 0 && modelName == "" {
		contextWindow = p.contextWindow
	}
	// Empty model names are retained for low-level callers and test doubles
	// that predate explicit model metadata. Production-created agents always
	// carry a model name and must resolve it before running.
	if contextWindow <= 0 && modelName == "" {
		contextWindow = defaultContextWindow
	}
	a := &Agent{
		id:            id,
		name:          opts.Name,
		lm:            lm,
		opts:          opts,
		pool:          p,
		modelName:     modelName,
		contextWindow: contextWindow,
		providerOpts:  opts.ProviderOptions,
	}
	// New agents inherit the base system prompt unless explicitly disabled.
	// Sub-agents that don't need the full orchestration context can set
	// InheritBasePrompt = &false to avoid carrying unnecessary overhead.
	inherit := opts.InheritBasePrompt == nil || *opts.InheritBasePrompt
	if inherit {
		p.baseSystemPromptMu.RLock()
		base := p.baseSystemPrompt
		p.baseSystemPromptMu.RUnlock()
		if base != "" {
			a.systemPrompt = base
		}
	}
	p.agents[id] = a
	p.nextSeq++
	p.spawnSeq[id] = p.nextSeq
	live := int64(len(p.agents))
	p.mu.Unlock()
	// Reported outside the lock: the observer is host code and must never be
	// called while holding the pool mutex.
	p.observeLifecycle(AgentLifecycle{
		AgentID: id,
		Main:    id == MainAgentID,
		Live:    live,
		Spawned: true,
	})
	return a, nil
}

// Get returns the Agent for id, or nil if not found.
// Thread-safe for concurrent reads.
func (p *AgentPool) Get(id string) *Agent {
	p.mu.RLock()
	defer p.mu.RUnlock()
	return p.agents[id]
}

// Close cancels the agent's active context and removes it and every agent
// beneath it from the pool. Returns ErrAgentNotFound if id is unknown.
//
// Closing cascades because an agent's descendants exist only to serve it:
// leaving them running after their parent is gone produces orphaned work whose
// results have nowhere to go. This applies to the root agent too, so stopping
// the root stops the whole fleet.
func (p *AgentPool) Close(id string) error {
	p.mu.Lock()
	root, exists := p.agents[id]
	if !exists {
		p.mu.Unlock()
		return ErrAgentNotFound
	}
	// Collect the subtree before mutating the map. Descendants are those whose
	// ID sits under "<id>/", which is the same convention spawn uses to derive
	// child IDs.
	doomed := []*Agent{root}
	for agentID, a := range p.agents {
		if agentID != id && isDescendantID(agentID, id) {
			doomed = append(doomed, a)
		}
	}
	closed := make([]string, 0, len(doomed))
	for _, a := range doomed {
		delete(p.agents, a.id)
		delete(p.spawnSeq, a.id)
		closed = append(closed, a.id)
	}
	live := int64(len(p.agents))
	p.mu.Unlock()

	// Report and cancel outside the lock: the observer is host code and Cancel
	// must not run under the pool mutex.
	for _, a := range doomed {
		p.observeLifecycle(AgentLifecycle{
			AgentID: a.id,
			Main:    a.id == MainAgentID,
			Live:    live,
			Spawned: false,
		})
		a.Cancel()
	}
	return nil
}

// isDescendantID reports whether agentID names an agent beneath ancestorID.
// IDs are built as "<scope>/<name>", so a descendant's ID is prefixed by
// "<ancestor>/". A bare prefix match would wrongly treat "main/x2" as a child
// of "main/x", hence the separator.
func isDescendantID(agentID, ancestorID string) bool {
	if ancestorID == "" {
		return false
	}
	return strings.HasPrefix(agentID, ancestorID+"/")
}

// Descendants returns the IDs of every agent beneath id, excluding id itself.
// Used by callers that need to report what a close or cancel will affect.
func (p *AgentPool) Descendants(id string) []string {
	p.mu.RLock()
	defer p.mu.RUnlock()
	var out []string
	for agentID := range p.agents {
		if isDescendantID(agentID, id) {
			out = append(out, agentID)
		}
	}
	sort.Strings(out)
	return out
}

// SendMessage appends msg to the named agent's inbox for delivery before its next turn.
// Returns ErrAgentNotFound if id is unknown, or an error if msg.Content is empty.
func (p *AgentPool) SendMessage(id string, msg sdk.Message) error {
	if strings.TrimSpace(msg.Content) == "" {
		return fmt.Errorf("SendMessage: content must be non-empty (would cause API rejection)")
	}
	p.mu.RLock()
	a, exists := p.agents[id]
	p.mu.RUnlock()
	if !exists {
		return ErrAgentNotFound
	}
	a.AppendInbox(msg)
	return nil
}

// TokenCount returns the total number of text tokens emitted across all agents
// since the pool was created. The counter is monotonically increasing.
func (p *AgentPool) TokenCount() int64 {
	return p.tokenCount.Load()
}

// addTokens increments the global token counter by n.
// Called by Agent.Submit's onToken closure.
func (p *AgentPool) addTokens(n int64) {
	p.tokenCount.Add(n)
}

// AddTokens is the exported counterpart of addTokens, exposed for testing.
func (p *AgentPool) AddTokens(n int64) {
	p.tokenCount.Add(n)
}

// ListAgents returns a snapshot of all registered agent IDs.
// ListAgents returns live agent IDs in spawn order. The pool stores agents in a
// map, so without the recorded sequence the order would be random and any list
// built from it would reshuffle between calls.
func (p *AgentPool) ListAgents() []string {
	p.mu.RLock()
	ids := make([]string, 0, len(p.agents))
	for id := range p.agents {
		ids = append(ids, id)
	}
	sort.Slice(ids, func(i, j int) bool {
		return p.spawnSeq[ids[i]] < p.spawnSeq[ids[j]]
	})
	p.mu.RUnlock()
	return ids
}

// ListTeams returns a snapshot of all registered team IDs.
func (p *AgentPool) ListTeams() []string {
	p.mu.RLock()
	defer p.mu.RUnlock()
	ids := make([]string, 0, len(p.teams))
	for id := range p.teams {
		ids = append(ids, id)
	}
	return ids
}

// GetTeamMembers returns the member agent IDs for the named team.
// Returns ErrTeamNotFound if the team does not exist.
func (p *AgentPool) GetTeamMembers(teamID string) ([]string, error) {
	p.mu.RLock()
	t, exists := p.teams[teamID]
	p.mu.RUnlock()
	if !exists {
		return nil, ErrTeamNotFound
	}
	return t.Members(), nil
}

// CreateTeam creates a new named Team associated with this pool.
// Returns ErrTeamExists if the ID is already taken.
func (p *AgentPool) CreateTeam(id string) (*Team, error) {
	p.mu.Lock()
	defer p.mu.Unlock()
	if _, exists := p.teams[id]; exists {
		return nil, ErrTeamExists
	}
	t := &Team{
		id:      id,
		pool:    p,
		members: make(map[string]bool),
	}
	p.teams[id] = t
	return t, nil
}

// GetTeam returns the Team for id, or nil if not found.
func (p *AgentPool) GetTeam(id string) *Team {
	p.mu.RLock()
	defer p.mu.RUnlock()
	return p.teams[id]
}

// Send calls Submit on the named agent with the given content, using a background context.
// Returns ErrAgentNotFound if id is unknown.
func (p *AgentPool) Send(id string, content string) error {
	p.mu.RLock()
	a, exists := p.agents[id]
	p.mu.RUnlock()
	if !exists {
		return ErrAgentNotFound
	}
	a.Submit(context.Background(), content)
	return nil
}

// SetAgentHistory replaces the conversation history of the named agent.
// Returns ErrAgentNotFound if id is unknown.
func (p *AgentPool) SetAgentHistory(id string, history []sdk.Message) error {
	p.mu.RLock()
	a, exists := p.agents[id]
	p.mu.RUnlock()
	if !exists {
		return ErrAgentNotFound
	}
	replacement := make([]sdk.Message, len(history))
	copy(replacement, history)
	a.historyMu.Lock()
	a.history = replacement
	a.historyMu.Unlock()
	return nil
}

// Cancel cancels the active turn of the named agent.
// Returns ErrAgentNotFound if id is unknown. No-op if no turn is running.
// Cancel stops the named agent's in-flight turn and every descendant's, for
// the same reason Close cascades: work beneath a stopped agent can never
// deliver its result. The agents stay in the pool so they can be inspected or
// reused; only their running turns stop.
func (p *AgentPool) Cancel(id string) error {
	p.mu.RLock()
	targets := make([]*Agent, 0, 1)
	if a, ok := p.agents[id]; ok {
		targets = append(targets, a)
	} else {
		p.mu.RUnlock()
		return ErrAgentNotFound
	}
	for agentID, a := range p.agents {
		if isDescendantID(agentID, id) {
			targets = append(targets, a)
		}
	}
	p.mu.RUnlock()
	for _, a := range targets {
		a.Cancel()
	}
	return nil
}

// CancelAll cancels the active turn of every agent in the pool.
func (p *AgentPool) CancelAll() {
	p.mu.RLock()
	agents := make([]*Agent, 0, len(p.agents))
	for _, a := range p.agents {
		agents = append(agents, a)
	}
	p.mu.RUnlock()
	for _, a := range agents {
		a.Cancel()
	}
}

// CloseTeam cancels all member agents and removes the team from the pool.
// Returns ErrTeamNotFound if id is unknown.
func (p *AgentPool) CloseTeam(ctx context.Context, id string) error {
	p.mu.Lock()
	t, exists := p.teams[id]
	if !exists {
		p.mu.Unlock()
		return ErrTeamNotFound
	}
	delete(p.teams, id)
	p.mu.Unlock()
	return t.Close(ctx)
}

// SetSubagentResolver installs the resolver used to choose a sub-agent's model
// when a spawn request omits one. It returns the language model and the
// resolved model name. A nil resolver restores the pool's default-model path.
// The resolver may select a provider different from the session's, which the
// pool's single-provider factory cannot express.
func (p *AgentPool) SetSubagentResolver(
	fn func(ctx context.Context, requested string) (fantasy.LanguageModel, string, error),
) {
	p.mu.Lock()
	p.subagentResolver = fn
	p.mu.Unlock()
}

// ResolveSubagentModel returns the language model for a spawn request. A
// non-empty requested name is resolved through the normal factory path so an
// explicit model always wins. An empty name consults the subagent resolver when
// one is installed (e.g. a configured working tier), and otherwise falls back
// to the pool's default model.
func (p *AgentPool) ResolveSubagentModel(
	ctx context.Context,
	requested, endpoint string,
) (fantasy.LanguageModel, string, error) {
	if requested != "" {
		lm, err := p.LanguageModelForModelAtEndpoint(ctx, requested, endpoint)
		return lm, requested, err
	}
	p.mu.RLock()
	resolver := p.subagentResolver
	p.mu.RUnlock()
	if resolver != nil {
		return resolver(ctx, requested)
	}
	lm, err := p.LanguageModelForModelAtEndpoint(ctx, "", endpoint)
	return lm, p.DefaultModelName(), err
}

// TurnUsage describes one completed agent turn for observability. It is a
// plain value so the agent package stays free of any metrics dependency: the
// host installs an observer via SetUsageObserver and decides what to record.
type TurnUsage struct {
	// AgentID is the agent that ran the turn (e.g. "main", "main/coder").
	AgentID string
	// Model is the model the turn actually ran on.
	Model string
	// Main reports whether this was the primary agent rather than a sub-agent.
	Main bool
	// Err reports whether the turn failed, so a failed turn is counted without
	// attributing token usage it never produced.
	Err bool
	// Token counts for the turn. Zero on failure.
	InputTokens         int64
	OutputTokens        int64
	TotalTokens         int64
	ReasoningTokens     int64
	CacheCreationTokens int64
	CacheReadTokens     int64
	// DurationMS is the turn's wall-clock duration in milliseconds.
	DurationMS int64
	// Started reports that the turn began, so observers can track in-flight
	// turns. When true the remaining fields are zero and no completion is
	// implied; a completion is reported by a subsequent call with Started false.
	Started bool
}

// TurnStarted reports a turn beginning. Observers use it to track in-flight
// work; no usage fields are populated.
func TurnStarted(agentID string, main bool) TurnUsage {
	return TurnUsage{AgentID: agentID, Main: main, Started: true}
}

// SetUsageObserver installs a callback invoked once per completed agent turn
// with that turn's usage. The observer runs on the agent's turn goroutine, so
// it must be safe for concurrent use and must not block. A nil observer
// disables reporting.
func (p *AgentPool) SetUsageObserver(fn func(TurnUsage)) {
	p.dispatchMu.Lock()
	p.usageObserver = fn
	p.dispatchMu.Unlock()
}

// observeTurn reports a completed turn to the installed observer, if any.
func (p *AgentPool) observeTurn(u TurnUsage) {
	if p == nil {
		return
	}
	p.dispatchMu.RLock()
	fn := p.usageObserver
	p.dispatchMu.RUnlock()
	if fn != nil {
		fn(u)
	}
}

// AgentLifecycle describes an agent appearing or disappearing from the pool,
// for observability. Reported outside the pool mutex.
type AgentLifecycle struct {
	// AgentID is the agent that changed (e.g. "main", "main/coder").
	AgentID string
	// Live is the number of agents in the pool after the change.
	Live int64
	// Main reports whether the agent is the primary agent.
	Main bool
	// Spawned is true when the agent was added, false when removed.
	Spawned bool
}

// SetLifecycleObserver installs a callback invoked whenever an agent is added
// to or removed from the pool. It runs on the caller's goroutine and must be
// safe for concurrent use and non-blocking. A nil observer disables reporting.
func (p *AgentPool) SetLifecycleObserver(fn func(AgentLifecycle)) {
	p.dispatchMu.Lock()
	p.lifecycleObserver = fn
	p.dispatchMu.Unlock()
}

// observeLifecycle reports a pool membership change to the installed observer.
// Callers must not hold p.mu: the observer is host code.
func (p *AgentPool) observeLifecycle(ev AgentLifecycle) {
	if p == nil {
		return
	}
	p.dispatchMu.RLock()
	fn := p.lifecycleObserver
	p.dispatchMu.RUnlock()
	if fn != nil {
		fn(ev)
	}
}
