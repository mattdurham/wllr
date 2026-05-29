package harness

// NOTE: Any changes to this file must be reflected in the corresponding SPECS.md or NOTES.md.

// harnessAgentBridge and harnessTeamBridge are unexported bridge adapter structs
// that implement the extension.AgentBridge and extension.TeamBridge interfaces
// by delegating to the agent pool. They are constructed in SetProgram and
// installed on the extension host via the Set* methods.

import (
	"context"
	"fmt"
	"strings"
	"time"

	tea "charm.land/bubbletea/v2"
	"github.com/mattdurham/wllr/agent"
	"github.com/mattdurham/wllr/extension"
	"github.com/mattdurham/wllr/sdk"
)

// earlyUIBridge is installed before SetProgram is called so that extensions
// can register commands during _init. All other methods are no-ops or return
// defaults, since the bubbletea program is not yet available.
type earlyUIBridge struct {
	cmds *Registry
}

func (e *earlyUIBridge) Notify(_ string)                                                  {}
func (e *earlyUIBridge) ShowModal(_ string)                                               {}
func (e *earlyUIBridge) ShowPicker(_ string, _ []sdk.ShowPickerItem, _ string)            {}
func (e *earlyUIBridge) Abort()                                                           {}
func (e *earlyUIBridge) SetStatus(_, _ string)                                            {}
func (e *earlyUIBridge) GetStatusInfo() sdk.StatusInfo                                    { return sdk.StatusInfo{Statuses: map[string]string{}} }
func (e *earlyUIBridge) SendMessage(_ sdk.Message)                                        {}
func (e *earlyUIBridge) RegisterCommand(name, desc string) error {
	e.cmds.Register(Command{
		Name: name,
		Desc: desc,
		Handler: func(args []string) tea.Cmd {
			return func() tea.Msg {
				return dispatchOnCommandMsg{Name: name, Args: args}
			}
		},
	})
	return nil
}
func (e *earlyUIBridge) RegisterTool(_ sdk.Tool) error                { return nil }
func (e *earlyUIBridge) SetSystemPrompt(_ string)                     {}
func (e *earlyUIBridge) AppendSystemPrompt(_ string)                  {}
func (e *earlyUIBridge) ResetHistory(_ []sdk.Message) error           { return nil }
func (e *earlyUIBridge) ToolResult(_, _ string, _ bool)               {}
func (e *earlyUIBridge) AfterToolCall(_, _, _ string, _ bool)         {}
func (e *earlyUIBridge) ConsoleOutput(_ string)                       {}
func (e *earlyUIBridge) ConsoleClear()                                {}

// Verify earlyUIBridge satisfies the interface at compile time.
var _ extension.UIBridge = (*earlyUIBridge)(nil)

// harnessAgentBridge implements extension.AgentBridge by delegating to the
// agent.AgentPool and agent.Spawner.
type harnessAgentBridge struct {
	pool    *agent.AgentPool
	spawner *agent.Spawner
	mainID  string
	prog    *tea.Program
}

func (b *harnessAgentBridge) Spawn(ctx context.Context, req extension.SpawnRequest) error {
	return b.spawner.Spawn(ctx, agent.SpawnRequest{
		ID:             req.ID,
		Name:           req.Name,
		SystemPrompt:   req.SystemPrompt,
		ModelName:      req.ModelName,
		InitialPrompt:  req.InitialPrompt,
		ThinkingBudget: req.ThinkingBudget,
	})
}

func (b *harnessAgentBridge) Close(id string) error {
	if b.pool == nil {
		return nil
	}
	return b.pool.Close(id)
}

func (b *harnessAgentBridge) SendMessage(id, message string) error {
	if b.pool == nil {
		return fmt.Errorf("no agent pool")
	}
	if strings.TrimSpace(message) == "" {
		return fmt.Errorf("send_message: message must be non-empty")
	}
	return b.pool.SendMessage(id, sdk.Message{Role: sdk.RoleUser, Content: message})
}

func (b *harnessAgentBridge) Run(id string) error {
	if b.pool == nil {
		return fmt.Errorf("no agent pool")
	}
	// Send agentWakeupMsg to set streaming=true for the TUI indicator.
	// Only fire for the main agent — sub-agents don't have TUI streaming state.
	if id == b.mainID {
		b.prog.Send(agentWakeupMsg{})
	}
	return b.pool.Send(id, "[process pending inbox messages]")
}

func (b *harnessAgentBridge) List() ([]extension.AgentInfo, error) {
	if b.pool == nil {
		return nil, nil
	}
	ids := b.pool.ListAgents()
	infos := make([]extension.AgentInfo, 0, len(ids))
	for _, id := range ids {
		agentName := id
		if a := b.pool.Get(id); a != nil {
			agentName = a.Name()
		}
		infos = append(infos, extension.AgentInfo{ID: id, Name: agentName})
	}
	return infos, nil
}

func (b *harnessAgentBridge) TokenCount() int64 {
	if b.pool == nil {
		return 0
	}
	return b.pool.TokenCount()
}

func (b *harnessAgentBridge) SetHistory(id string, messages []sdk.Message) error {
	if b.pool == nil {
		return fmt.Errorf("no agent pool")
	}
	return b.pool.SetAgentHistory(id, messages)
}

// harnessTeamBridge implements extension.TeamBridge by delegating to the agent pool.
type harnessTeamBridge struct {
	pool *agent.AgentPool
}

func (b *harnessTeamBridge) Create(id, _ string) error {
	if b.pool == nil {
		return fmt.Errorf("no agent pool")
	}
	_, err := b.pool.CreateTeam(id)
	return err
}

func (b *harnessTeamBridge) Close(_ context.Context, id string) error {
	if b.pool == nil {
		return nil
	}
	return b.pool.CloseTeam(context.Background(), id)
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

// harnessUIBridge implements extension.UIBridge by sending bubbletea messages.
type harnessUIBridge struct {
	pool   *agent.AgentPool
	prog   *tea.Program
	live   *liveState
	mainID string
	cmds   *Registry
}

func (b *harnessUIBridge) Notify(text string) {
	b.prog.Send(NotifyMsg{Text: text})
}

func (b *harnessUIBridge) ShowModal(text string) {
	b.prog.Send(ShowModalMsg{Text: text})
}

func (b *harnessUIBridge) ShowPicker(title string, items []sdk.ShowPickerItem, callback string) {
	b.prog.Send(ShowPickerMsg{Title: title, Items: items, Callback: callback})
}

func (b *harnessUIBridge) Abort() {
	b.prog.Send(abortStreamMsg{})
}

func (b *harnessUIBridge) SetStatus(key, value string) {
	b.prog.Send(StatusUpdateMsg{Key: key, Value: value})
}

func (b *harnessUIBridge) GetStatusInfo() sdk.StatusInfo {
	live := b.live
	live.mu.RLock()
	streaming := live.streaming
	streamStart := live.streamStart
	width := live.width
	hasError := live.hasError
	tokens := live.tokens
	provider := live.provider
	modelName := live.model
	statuses := make(map[string]string, len(live.statuses))
	for k, v := range live.statuses {
		statuses[k] = v
	}
	live.mu.RUnlock()

	info := sdk.StatusInfo{
		Provider: provider,
		Model:    modelName,
		Tokens:   tokens,
		Working:  streaming,
		Statuses: statuses,
		Width:    width,
		HasError: hasError,
	}
	if streaming {
		info.ElapsedMs = time.Since(streamStart).Milliseconds()
	}
	if b.pool != nil {
		n := len(b.pool.ListAgents()) - 1
		if n < 0 {
			n = 0
		}
		info.ActiveAgents = n
	}
	return info
}

func (b *harnessUIBridge) SendMessage(msg sdk.Message) {
	sm := SubmitMsg{Content: msg.Content}
	if strings.HasPrefix(strings.TrimSpace(msg.Content), "<skill ") {
		sm.Display = skillDisplayName(msg.Content)
	}
	b.prog.Send(sm)
}

func (b *harnessUIBridge) RegisterCommand(name, desc string) error {
	b.cmds.Register(Command{
		Name: name,
		Desc: desc,
		Handler: func(args []string) tea.Cmd {
			return func() tea.Msg {
				return dispatchOnCommandMsg{Name: name, Args: args}
			}
		},
	})
	return nil
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

func (b *harnessUIBridge) AppendSystemPrompt(text string) {
	if b.pool != nil {
		b.pool.AppendBaseSystemPrompt(text)
	}
}

func (b *harnessUIBridge) ResetHistory(messages []sdk.Message) error {
	if b.pool == nil {
		return fmt.Errorf("no agent pool")
	}
	if err := b.pool.SetAgentHistory(b.mainID, messages); err != nil {
		return err
	}
	b.prog.Send(ResetHistoryMsg{Messages: messages})
	return nil
}

func (b *harnessUIBridge) ToolResult(toolCallID, result string, isError bool) {
	// Tool result visual feedback is handled via AfterToolCall; this is a no-op here.
	_ = toolCallID
	_ = result
	_ = isError
}

func (b *harnessUIBridge) AfterToolCall(id, _, result string, isError bool) {
	b.prog.Send(ToolCallDoneMsg{ID: id, IsError: isError, Output: result})
}

func (b *harnessUIBridge) ConsoleOutput(line string) {
	b.prog.Send(ConsoleMsg{Line: line})
}

func (b *harnessUIBridge) ConsoleClear() {
	b.prog.Send(ConsoleMsg{Clear: true})
}
