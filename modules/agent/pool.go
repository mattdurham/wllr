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
	"strconv"
	"strings"

	"charm.land/fantasy"
	"github.com/mattdurham/wllr/modules/sdk"
)

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
func (p *AgentPool) SetContextUsageDispatcher(fn func(cu sdk.ContextUsage, compact bool, thresholdPct float64, compactions int)) {
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
	if p.provider == nil {
		return nil, errors.New("agent: no provider configured on pool")
	}
	if model == "" {
		p.mu.RLock()
		model = p.defaultModelName
		p.mu.RUnlock()
	}
	return p.provider.LanguageModel(ctx, model)
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
	p.mu.Lock()
	defer p.mu.Unlock()
	if _, exists := p.agents[id]; exists {
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
	return a, nil
}

// Get returns the Agent for id, or nil if not found.
// Thread-safe for concurrent reads.
func (p *AgentPool) Get(id string) *Agent {
	p.mu.RLock()
	defer p.mu.RUnlock()
	return p.agents[id]
}

// Close cancels the agent's active context and removes it from the pool.
// Returns ErrAgentNotFound if id is unknown.
func (p *AgentPool) Close(id string) error {
	p.mu.Lock()
	a, exists := p.agents[id]
	if !exists {
		p.mu.Unlock()
		return ErrAgentNotFound
	}
	delete(p.agents, id)
	p.mu.Unlock()
	// Cancel any running turn.
	a.Cancel()
	return nil
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
func (p *AgentPool) ListAgents() []string {
	p.mu.RLock()
	defer p.mu.RUnlock()
	ids := make([]string, 0, len(p.agents))
	for id := range p.agents {
		ids = append(ids, id)
	}
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
func (p *AgentPool) Cancel(id string) error {
	p.mu.RLock()
	a, exists := p.agents[id]
	p.mu.RUnlock()
	if !exists {
		return ErrAgentNotFound
	}
	a.Cancel()
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
