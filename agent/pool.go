// Package agent manages sub-agents and teams for the bob harness.
// Each Agent wraps a fantasy.LanguageModel run loop with a message inbox.
// AgentPool owns all live agents and a shared token counter.
package agent

// NOTE: Any changes to this file must be reflected in the corresponding SPECS.md or NOTES.md.

import (
	"context"
	"errors"
	"fmt"
	"strings"

	"charm.land/fantasy"
	"github.com/mattdurham/wllr/sdk"
)

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

// contextWindow is the model's input context window in tokens.
// Set via SetContextWindow; defaults to 0 (compaction uses model-name fallback).

// tokenCount accumulates all text tokens emitted by all agents in this pool.

// baseSystemPrompt is accumulated from set_system_prompt / append_system_prompt
// and applied to every agent (current and future) so all agents share the
// same base context (AGENTS.md, skill list, etc.).

// NewPool creates an empty AgentPool.
func NewPool() *AgentPool {
	return &AgentPool{
		agents: make(map[string]*Agent),
		teams:  make(map[string]*Team),
	}
}

// SetProvider stores the fantasy provider so OnAgentSpawn wiring in cmd/main.go
// can call LanguageModelForModel to create sub-agent language models.
func (p *AgentPool) SetProvider(prov fantasy.Provider) {
	p.provider = prov
}

// SetContextWindow sets the model's input context window in tokens.
// When non-zero this overrides the model-name-based lookup in compaction.
func (p *AgentPool) SetContextWindow(tokens int64) {
	p.mu.Lock()
	p.contextWindow = tokens
	p.mu.Unlock()
}

// ContextWindow returns the configured context window, or 0 if unset.
func (p *AgentPool) ContextWindow() int64 {
	p.mu.RLock()
	defer p.mu.RUnlock()
	return p.contextWindow
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
	p.mu.Unlock()
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
	a := &Agent{
		id:             id,
		name:           opts.Name,
		lm:             lm,
		opts:           opts,
		pool:           p,
		modelName:      modelName,
		notifyParentID: opts.NotifyParentID,
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
