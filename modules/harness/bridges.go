package harness

// NOTE: Any changes to this file must be reflected in the corresponding SPECS.md or NOTES.md.

// This file contains the bridge adapter types installed on the extension host:
//   - earlyUIBridge: stub UIBridge installed in New() before a front end exists;
//     supports command registration during extension _init.
//   - earlyAgentBridge: stub AgentBridge installed in New() before a front end
//     exists; returns descriptive errors for any agent operation during _init.
//   - harnessAgentBridge: full AgentBridge; delegates to agent.Spawner and the
//     agent pool, and reports wakeups to the front-end sink.
//   - harnessTeamBridge: full TeamBridge; delegates to the pool.
//   - harnessUIBridge: full UIBridge; performs the engine-local mutations
//     (system prompt, history, tool-call bookkeeping) directly and forwards
//     every user-visible effect to the renderer-neutral sdk.UISink.
//
// None of the full bridges depends on a terminal. The interactive TUI installs a
// teaSink (turning effects into bubbletea messages); the HTTP server installs a
// HubSink (publishing frames to clients); an embedder installs an sdk.FuncSink.
// This is what lets a non-terminal front end drive the same engine — in
// particular it lets serve mode install the real AgentBridge, so create_agent
// works headlessly.

import (
	"context"
	"fmt"
	"strings"
	"time"

	tea "charm.land/bubbletea/v2"
	"github.com/mattdurham/wllr/modules/agent"
	"github.com/mattdurham/wllr/modules/extension"
	"github.com/mattdurham/wllr/modules/sdk"
)

// earlyUIBridge is installed before a front end is set so that extensions
// can register commands during _init. All other methods are no-ops or return
// defaults, since no front end is available yet.
type earlyUIBridge struct {
	cmds *Registry
}

func (e *earlyUIBridge) Notify(_ string)                         {}
func (e *earlyUIBridge) ShowModal(_ string)                      {}
func (e *earlyUIBridge) ShowPicker(_ sdk.ShowPickerParams)       {}
func (e *earlyUIBridge) ShowAgentTree(_ sdk.ShowAgentTreeParams) {}
func (e *earlyUIBridge) SetFocusedAgent(_ string)                {}
func (e *earlyUIBridge) ShowTextInput(_, _, _, _ string)         {}
func (e *earlyUIBridge) Abort()                                  {}
func (e *earlyUIBridge) SetStatus(_, _ string)                   {}
func (e *earlyUIBridge) GetStatusInfo() sdk.StatusInfo {
	return sdk.StatusInfo{Statuses: map[string]string{}}
}
func (e *earlyUIBridge) SendMessage(_ sdk.Message) {}
func (e *earlyUIBridge) RegisterCommand(name, desc string, instant bool) error {
	e.cmds.Register(Command{
		Name:    name,
		Desc:    desc,
		Instant: instant,
		Handler: func(args []string) tea.Cmd {
			return func() tea.Msg {
				return dispatchOnCommandMsg{Name: name, Args: args}
			}
		},
	})
	return nil
}
func (e *earlyUIBridge) RegisterTool(_ sdk.Tool) error                           { return nil }
func (e *earlyUIBridge) SetSystemPrompt(_ string)                                {}
func (e *earlyUIBridge) AppendSystemPromptFrom(_, _ string)                      {}
func (e *earlyUIBridge) SetSystemPromptComponents(_ []sdk.SystemPromptComponent) {}
func (e *earlyUIBridge) SetModel(_, _ string) error                              { return nil }
func (e *earlyUIBridge) ResetHistory(_ []sdk.Message) error                      { return nil }
func (e *earlyUIBridge) ToolResult(_, _ string, _ bool)                          {}
func (e *earlyUIBridge) AfterToolCall(_, _, _, _ string, _ bool)                 {}
func (e *earlyUIBridge) ConsoleOutput(_ string)                                  {}
func (e *earlyUIBridge) ConsoleClear()                                           {}
func (e *earlyUIBridge) CreateArea(_ sdk.UIArea) error                           { return nil }
func (e *earlyUIBridge) PatchUI(_ sdk.UIPatchParams) error                       { return nil }
func (e *earlyUIBridge) RemoveArea(_ string)                                     {}
func (e *earlyUIBridge) UpdateArea(_ sdk.UIUpdateAreaParams) error               { return nil }

// Verify earlyUIBridge satisfies the interface at compile time.
var _ extension.UIBridge = (*earlyUIBridge)(nil)

// earlyAgentBridge is installed before a front end is set so that extensions
// that call agent_spawn during _init receive a clear error rather than a nil
// pointer dereference. All methods return descriptive errors.
type earlyAgentBridge struct{}

func (e *earlyAgentBridge) Spawn(_ context.Context, _ extension.SpawnRequest) error {
	return fmt.Errorf("agent_spawn: session not yet started")
}

func (e *earlyAgentBridge) GetHistory(_ string) ([]sdk.Message, error) {
	return nil, fmt.Errorf("agent_get_history: session not yet started")
}
func (e *earlyAgentBridge) Close(_ string) error { return fmt.Errorf("not started") }
func (e *earlyAgentBridge) SendMessage(_ string, _ sdk.Message) error {
	return fmt.Errorf("not started")
}

func (e *earlyAgentBridge) Deliver(_ string, _ sdk.Message, _ bool) error {
	return fmt.Errorf("not started")
}

func (e *earlyAgentBridge) Run(_ string) error                      { return fmt.Errorf("not started") }
func (e *earlyAgentBridge) List() ([]extension.AgentInfo, error)    { return nil, nil }
func (e *earlyAgentBridge) TokenCount() int64                       { return 0 }
func (e *earlyAgentBridge) MainAgentContextUsage() sdk.ContextUsage { return sdk.ContextUsage{} }
func (e *earlyAgentBridge) SetHistory(_ string, _ []sdk.Message) error {
	return fmt.Errorf("not started")
}

func (e *earlyAgentBridge) SnapshotInbox(_ string) ([]sdk.Message, error) {
	return nil, fmt.Errorf("not started")
}

func (e *earlyAgentBridge) DeleteFromInbox(_ string, _ int, _ string) (int, error) {
	return 0, fmt.Errorf("not started")
}

func (e *earlyAgentBridge) ClearInbox(_ string) (int, error) {
	return 0, fmt.Errorf("not started")
}

func (e *earlyAgentBridge) EditInboxMessage(_ string, _ int, _ string, _ string) error {
	return fmt.Errorf("not started")
}

// Verify earlyAgentBridge satisfies the interface at compile time.
var _ extension.AgentBridge = (*earlyAgentBridge)(nil)

// harnessAgentBridge implements extension.AgentBridge by delegating to the
// agent.AgentPool and agent.Spawner. Wakeups are reported to the front-end sink
// so it can show a "working" indicator; a nil sink disables only that hint.
type harnessAgentBridge struct {
	pool    *agent.AgentPool
	spawner *agent.Spawner
	sink    sdk.UISink
	mainID  string
}

func (b *harnessAgentBridge) Spawn(ctx context.Context, req extension.SpawnRequest) error {
	if b.spawner == nil {
		return fmt.Errorf("agent_spawn: spawner not initialized")
	}
	return b.spawner.Spawn(ctx, req)
}

func (b *harnessAgentBridge) Close(id string) error {
	if b.pool == nil {
		return nil
	}
	return b.pool.Close(id)
}

func (b *harnessAgentBridge) SendMessage(id string, msg sdk.Message) error {
	if b.pool == nil {
		return fmt.Errorf("no agent pool")
	}
	if strings.TrimSpace(msg.Content) == "" {
		return fmt.Errorf("send_message: message must be non-empty")
	}
	return b.pool.SendMessage(id, msg)
}

func (b *harnessAgentBridge) Deliver(id string, msg sdk.Message, wake bool) error {
	if b.pool == nil {
		return fmt.Errorf("no agent pool")
	}
	if strings.TrimSpace(msg.Content) == "" {
		return fmt.Errorf("deliver: message must be non-empty")
	}
	return b.pool.Deliver(id, msg, wake)
}

func (b *harnessAgentBridge) Run(id string) error {
	if b.pool == nil {
		return fmt.Errorf("no agent pool")
	}
	// Tell the front end the root agent is waking. Only the root has stream
	// state to indicate; sub-agents do not.
	if id == b.mainID && b.sink != nil {
		b.sink.AgentWakeup(id)
	}
	// Empty content: the agent drains its inbox (queued by a prior SendMessage)
	// as the turn content. No synthetic placeholder message leaks into history.
	return b.pool.Send(id, "")
}

func (b *harnessAgentBridge) List() ([]extension.AgentInfo, error) {
	if b.pool == nil {
		return nil, nil
	}
	ids := b.pool.ListAgents()
	infos := make([]extension.AgentInfo, 0, len(ids))
	for _, id := range ids {
		agentName := id
		isRunning := false
		pendingMessages := int64(0)
		var activityAgeMS int64
		var turnDurationMS int64
		var lastToolAgeMS int64
		var lastToolDoneAgeMS int64
		var activeTool string
		var lastTool string
		liveness := "idle"
		working := false
		var shutdownRequested bool
		if a := b.pool.Get(id); a != nil {
			agentName = a.Name()
			isRunning = a.IsRunning()
			pendingMessages = int64(a.InboxLen())
			activity := a.Activity()
			now := time.Now()
			if !activity.LastActivityAt.IsZero() {
				activityAgeMS = now.Sub(activity.LastActivityAt).Milliseconds()
			}
			if isRunning && !activity.TurnStartedAt.IsZero() {
				turnDurationMS = now.Sub(activity.TurnStartedAt).Milliseconds()
			}
			if !activity.LastToolCallAt.IsZero() {
				lastToolAgeMS = now.Sub(activity.LastToolCallAt).Milliseconds()
			}
			if !activity.LastToolDoneAt.IsZero() {
				lastToolDoneAgeMS = now.Sub(activity.LastToolDoneAt).Milliseconds()
			}
			activeTool = activity.ActiveToolName
			lastTool = activity.LastToolName
			shutdownRequested = activity.ShutdownRequested
			if isRunning {
				liveness = "working"
				working = true
			}
			if shutdownRequested {
				liveness = "stopping"
			}
		}
		infos = append(infos, extension.AgentInfo{
			ID:                id,
			Name:              agentName,
			IsRunning:         isRunning,
			PendingMessages:   pendingMessages,
			LastActivityAgeMS: activityAgeMS,
			TurnDurationMS:    turnDurationMS,
			LastToolAgeMS:     lastToolAgeMS,
			LastToolDoneAgeMS: lastToolDoneAgeMS,
			ActiveTool:        activeTool,
			LastTool:          lastTool,
			Liveness:          liveness,
			Working:           working,
			ShutdownRequested: shutdownRequested,
		})
	}
	return infos, nil
}

func (b *harnessAgentBridge) TokenCount() int64 {
	if b.pool == nil {
		return 0
	}
	return b.pool.TokenCount()
}

// GetHistory returns a copy of the named agent's conversation history. An
// empty id means the root agent, so callers do not need to know its ID.
func (b *harnessAgentBridge) GetHistory(id string) ([]sdk.Message, error) {
	if b.pool == nil {
		return nil, fmt.Errorf("no agent pool")
	}
	if id == "" {
		id = b.mainID
	}
	a := b.pool.Get(id)
	if a == nil {
		return nil, fmt.Errorf("agent %q not found", id)
	}
	return a.History(), nil
}

func (b *harnessAgentBridge) SetHistory(id string, messages []sdk.Message) error {
	if b.pool == nil {
		return fmt.Errorf("no agent pool")
	}
	return b.pool.SetAgentHistory(id, messages)
}

func (b *harnessAgentBridge) MainAgentContextUsage() sdk.ContextUsage {
	if b.pool == nil {
		return sdk.ContextUsage{}
	}
	return b.pool.MainAgentContextUsage()
}

func (b *harnessAgentBridge) SnapshotInbox(id string) ([]sdk.Message, error) {
	if b.pool == nil {
		return nil, fmt.Errorf("no agent pool")
	}
	return b.pool.SnapshotInbox(id)
}

func (b *harnessAgentBridge) DeleteFromInbox(id string, byIndex int, byMessageID string) (int, error) {
	if b.pool == nil {
		return 0, fmt.Errorf("no agent pool")
	}
	return b.pool.DeleteFromInbox(id, byIndex, byMessageID)
}

func (b *harnessAgentBridge) ClearInbox(id string) (int, error) {
	if b.pool == nil {
		return 0, fmt.Errorf("no agent pool")
	}
	return b.pool.ClearInbox(id)
}

func (b *harnessAgentBridge) EditInboxMessage(id string, byIndex int, byMessageID string, newContent string) error {
	if b.pool == nil {
		return fmt.Errorf("no agent pool")
	}
	return b.pool.EditInboxMessage(id, byIndex, byMessageID, newContent)
}

// harnessTeamBridge implements extension.TeamBridge by delegating to the agent pool.
type harnessTeamBridge struct {
	pool *agent.AgentPool
}

// Create creates a new team with the given id. The name parameter is accepted
// by the TeamBridge interface but is not used by the pool's CreateTeam implementation.
func (b *harnessTeamBridge) Create(id, _ string) error {
	if b.pool == nil {
		return fmt.Errorf("no agent pool")
	}
	_, err := b.pool.CreateTeam(id)
	return err
}

func (b *harnessTeamBridge) Close(ctx context.Context, id string) error {
	if b.pool == nil {
		return nil
	}
	return b.pool.CloseTeam(ctx, id)
}

func (b *harnessTeamBridge) AddMember(teamID, agentID string) error {
	if b.pool == nil {
		return fmt.Errorf("no agent pool")
	}
	t := b.pool.GetTeam(teamID)
	if t == nil {
		return fmt.Errorf("team not found: %s", teamID)
	}
	return t.AddMember(agentID)
}

func (b *harnessTeamBridge) RemoveMember(teamID, agentID string) error {
	if b.pool == nil {
		return nil
	}
	t := b.pool.GetTeam(teamID)
	if t == nil {
		// No-op: removing from a non-existent team is safe.
		return nil
	}
	t.RemoveMember(agentID)
	return nil
}

func (b *harnessTeamBridge) GetMembers(teamID string) ([]string, error) {
	if b.pool == nil {
		return nil, fmt.Errorf("no agent pool")
	}
	return b.pool.GetTeamMembers(teamID)
}

func (b *harnessTeamBridge) List() ([]string, error) {
	if b.pool == nil {
		return nil, nil
	}
	return b.pool.ListTeams(), nil
}

// harnessUIBridge implements extension.UIBridge. It performs the engine-local
// mutations (base system prompt, history reset, tool-call bookkeeping) itself
// and forwards every user-visible effect to the renderer-neutral sink. The same
// bridge therefore serves the TUI, the HTTP server, and an embedder — only the
// sink differs.
type harnessUIBridge struct {
	pool   *agent.AgentPool
	sink   sdk.UISink
	mainID string
}

func (b *harnessUIBridge) Notify(text string) {
	sinkOrNil(b.sink).Notify(text)
}

func (b *harnessUIBridge) ShowModal(text string) {
	sinkOrNil(b.sink).ShowModal(text)
}

func (b *harnessUIBridge) ShowPicker(params sdk.ShowPickerParams) {
	sinkOrNil(b.sink).ShowPicker(params)
}

// ShowAgentTree opens the interactive agent tree; a node selection is routed
// back to the extension as EventOnCommand with the given callback.
func (b *harnessUIBridge) ShowAgentTree(params sdk.ShowAgentTreeParams) {
	sinkOrNil(b.sink).ShowAgentTree(params)
}

// SetFocusedAgent tells the front end which agent user input and the transcript
// target.
func (b *harnessUIBridge) SetFocusedAgent(id string) {
	sinkOrNil(b.sink).FocusAgent(id)
}

func (b *harnessUIBridge) ShowTextInput(title, placeholder, initialValue, callback string) {
	sinkOrNil(b.sink).ShowTextInput(title, placeholder, initialValue, callback)
}

func (b *harnessUIBridge) Abort() {
	sinkOrNil(b.sink).Abort()
}

func (b *harnessUIBridge) SetStatus(key, value string) {
	sinkOrNil(b.sink).SetStatus(key, value)
}

func (b *harnessUIBridge) GetStatusInfo() sdk.StatusInfo {
	return sinkOrNil(b.sink).StatusInfo()
}

func (b *harnessUIBridge) SendMessage(msg sdk.Message) {
	display := ""
	if strings.HasPrefix(strings.TrimSpace(msg.Content), "<skill ") {
		display = skillDisplayName(msg.Content)
	}
	sinkOrNil(b.sink).Submit(msg.Content, display)
}

// SetModel queues a model switch on the front end. value may be a model ID or a
// configured model-tier name. Errors surface as notifications, so the returned
// error is always nil (the switch is asynchronous by design).
func (b *harnessUIBridge) SetModel(value, thinking string) error {
	return sinkOrNil(b.sink).SetModel(value, thinking)
}

func (b *harnessUIBridge) RegisterCommand(name, desc string, instant bool) error {
	return sinkOrNil(b.sink).RegisterCommand(name, desc, instant)
}

func (b *harnessUIBridge) RegisterTool(_ sdk.Tool) error {
	// Tool registration is handled by the extension host registry.
	// The UI layer doesn't need to track tools.
	return nil
}

func (b *harnessUIBridge) SetSystemPrompt(prompt string) {
	if b.pool != nil {
		b.pool.SetBaseSystemPrompt(prompt)
	}
}

func (b *harnessUIBridge) AppendSystemPromptFrom(source, text string) {
	if b.pool != nil {
		b.pool.AppendBaseSystemPromptFrom(source, text)
	}
}

func (b *harnessUIBridge) SetSystemPromptComponents(components []sdk.SystemPromptComponent) {
	if b.pool != nil {
		b.pool.SetBaseSystemPromptComponents(components)
	}
}

func (b *harnessUIBridge) ResetHistory(messages []sdk.Message) error {
	if b.pool == nil {
		return fmt.Errorf("no agent pool")
	}
	if err := b.pool.SetAgentHistory(b.mainID, messages); err != nil {
		return err
	}
	sinkOrNil(b.sink).ResetHistory(messages)
	return nil
}

func (b *harnessUIBridge) ToolResult(toolCallID, result string, isError bool) {
	// Tool result visual feedback is handled via AfterToolCall; this is a no-op here.
	_ = toolCallID
	_ = result
	_ = isError
}

func (b *harnessUIBridge) AfterToolCall(agentID, id, toolName, result string, isError bool) {
	if b.pool != nil {
		if a := b.pool.Get(agentID); a != nil {
			a.MarkToolCallDone(id, toolName)
		}
	}
	sinkOrNil(b.sink).ToolCallDone(agentID, id, toolName, result, isError)
}

func (b *harnessUIBridge) ConsoleOutput(line string) {
	sinkOrNil(b.sink).Console(line, false)
}

func (b *harnessUIBridge) ConsoleClear() {
	sinkOrNil(b.sink).Console("", true)
}

// CreateArea registers a new scene-graph area through the sink, which owns the
// scene graph and triggers a re-render on the front end.
func (b *harnessUIBridge) CreateArea(area sdk.UIArea) error {
	return sinkOrNil(b.sink).SceneCreate(area)
}

func (b *harnessUIBridge) PatchUI(params sdk.UIPatchParams) error {
	return sinkOrNil(b.sink).ScenePatch(params)
}

func (b *harnessUIBridge) RemoveArea(id string) {
	sinkOrNil(b.sink).SceneRemove(id)
}

func (b *harnessUIBridge) UpdateArea(params sdk.UIUpdateAreaParams) error {
	return sinkOrNil(b.sink).SceneUpdate(params)
}

// Verify harnessUIBridge satisfies the interface at compile time.
var _ extension.UIBridge = (*harnessUIBridge)(nil)

// sinkOrNil returns s, or a no-op sink when s is nil, so a bridge built without
// a front end degrades to silent no-ops instead of panicking.
func sinkOrNil(s sdk.UISink) sdk.UISink {
	if s == nil {
		return noopSink{}
	}
	return s
}

// noopSink is the do-nothing UISink used when a bridge has no front end.
type noopSink struct{}

var _ sdk.UISink = noopSink{}

func (noopSink) Notify(string)                             {}
func (noopSink) ShowModal(string)                          {}
func (noopSink) ShowPicker(sdk.ShowPickerParams)           {}
func (noopSink) ShowAgentTree(sdk.ShowAgentTreeParams)     {}
func (noopSink) FocusAgent(string)                         {}
func (noopSink) ShowTextInput(_, _, _, _ string)           {}
func (noopSink) Abort()                                    {}
func (noopSink) SetStatus(_, _ string)                     {}
func (noopSink) StatusInfo() sdk.StatusInfo                { return sdk.StatusInfo{Statuses: map[string]string{}} }
func (noopSink) Submit(_, _ string)                        {}
func (noopSink) SetModel(_, _ string) error                { return nil }
func (noopSink) RegisterCommand(_, _ string, _ bool) error { return nil }
func (noopSink) Console(_ string, _ bool)                  {}
func (noopSink) ResetHistory([]sdk.Message)                {}
func (noopSink) Token(string)                              {}
func (noopSink) ToolCallStart(_, _, _, _ string)           {}
func (noopSink) ToolCallDone(_, _, _, _ string, _ bool)    {}
func (noopSink) StreamDone(error)                          {}
func (noopSink) AgentWakeup(string)                        {}
func (noopSink) SceneCreate(sdk.UIArea) error              { return nil }
func (noopSink) ScenePatch(sdk.UIPatchParams) error        { return nil }
func (noopSink) SceneUpdate(sdk.UIUpdateAreaParams) error  { return nil }
func (noopSink) SceneRemove(string)                        {}
