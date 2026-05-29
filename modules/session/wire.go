package session

// NOTE: Any changes to this file must be reflected in the corresponding SPECS.md or NOTES.md.

import (
	"context"
	"encoding/json"
	"fmt"

	"github.com/mattdurham/wllr/modules/agent"
	"github.com/mattdurham/wllr/modules/extension"
	"github.com/mattdurham/wllr/modules/harness"
	"github.com/mattdurham/wllr/modules/sdk"
)

// ConversationSession implements Session.
// It manages the lifecycle of a single conversation session: start, submit,
// cancel, reload, and close. Bridge installation on the extension host is
// performed by the harness (earlyUIBridge/earlyAgentBridge in New, full bridges
// in SetProgram) and by cmd/main.go (CapabilityProvider). Wire itself is passive.
type ConversationSession struct {
	host     *extension.Host
	pool     *agent.AgentPool
	mainID   string
	renderer harness.Renderer
}

// Wire creates and returns a Session. Wire does not install any interface bridges
// on the host; that is the caller's responsibility (see harness.Model.SetProgram).
func Wire(host *extension.Host, pool *agent.AgentPool, mainAgentID string, renderer harness.Renderer) Session {
	return &ConversationSession{
		host:     host,
		pool:     pool,
		mainID:   mainAgentID,
		renderer: renderer,
	}
}

// Start fires session_start events to all loaded extensions.
// System prompt assembly occurs in the harness sessionStartDoneMsg handler, not here.
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
