package session

// NOTE: Any changes to this file must be reflected in the corresponding SPECS.md or NOTES.md.

import (
	"context"
	"encoding/json"
	"fmt"

	"github.com/mattdurham/wllr/agent"
	"github.com/mattdurham/wllr/extension"
	"github.com/mattdurham/wllr/harness"
	"github.com/mattdurham/wllr/sdk"
)

// ConversationSession implements Session.
// It manages the lifecycle of a single conversation session: start, submit,
// cancel, reload, and close. Subsystem wiring (bridge setters on the extension
// host) is assumed to have been performed by the caller before Wire is invoked.
type ConversationSession struct {
	host     *extension.Host
	pool     *agent.AgentPool
	mainID   string
	renderer harness.Renderer
}

// Wire creates and returns a Session.
// The host's interface bridges must be installed before calling Wire.
// Wire does not install any callbacks itself; that is the caller's responsibility.
func Wire(host *extension.Host, pool *agent.AgentPool, mainAgentID string, renderer harness.Renderer) Session {
	return &ConversationSession{
		host:     host,
		pool:     pool,
		mainID:   mainAgentID,
		renderer: renderer,
	}
}

// Start fires session_start events and assembles the initial system prompt.
// Must be called once before Submit.
func (s *ConversationSession) Start(ctx context.Context) error {
	if s.host == nil {
		return nil
	}
	payload, _ := json.Marshal(sdk.SessionStartPayload{Reason: "new_session"})
	evt := sdk.Event{Type: sdk.EventSessionStart, Payload: payload}
	results, err := s.host.DispatchEvent(ctx, evt)
	if err != nil {
		return fmt.Errorf("session start: %w", err)
	}
	for _, r := range results {
		if r.Error != "" && s.renderer != nil {
			s.renderer.AddNotification(fmt.Sprintf("Extension error: %s", r.Error))
		}
	}
	return nil
}

// Submit sends user input to the main agent. Non-blocking.
func (s *ConversationSession) Submit(_ context.Context, content, _ string) error {
	if s.pool == nil {
		return fmt.Errorf("no agent pool")
	}
	return s.pool.Send(s.mainID, content)
}

// Cancel cancels the active agent turn. No-op if no turn is in progress.
func (s *ConversationSession) Cancel() {
	if s.pool != nil {
		s.pool.CancelAll()
	}
}

// ReloadExtensions hot-reloads all WASM extensions from the given paths.
func (s *ConversationSession) ReloadExtensions(ctx context.Context, paths []string) error {
	if s.host == nil {
		return nil
	}
	return s.host.Reload(ctx, paths)
}

// Close shuts down agents, extensions, and releases resources.
func (s *ConversationSession) Close(ctx context.Context) error {
	if s.pool != nil {
		s.pool.CancelAll()
	}
	if s.host != nil {
		return s.host.Close(ctx)
	}
	return nil
}
