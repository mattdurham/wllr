package harness

// NOTE: Any changes to this file must be reflected in the corresponding SPECS.md or NOTES.md.

import (
	"fmt"
	"strings"
	"time"

	tea "charm.land/bubbletea/v2"
	"github.com/mattdurham/wllr/modules/agent"
	"github.com/mattdurham/wllr/modules/sdk"
)

// teaSink is the interactive-TUI implementation of sdk.UISink. It turns every
// renderer-neutral effect back into the bubbletea message the TUI already
// understood, so replacing the bridges' direct *tea.Program dependency with the
// sink is behavior-preserving: the same messages reach the same Update cases.
//
// It owns the two pieces of presentation state the UI bridge used to hold
// directly: the shared liveState (read by statusline extensions through
// StatusInfo) and the SceneRenderer that the extension scene-graph surface
// drives. The Model shares both pointers for its own rendering.
//
// Every method is safe for concurrent use: sends go through tea.Program.Send,
// which is the program's own concurrency-safe fan-in, and the SceneRenderer is
// internally locked.
type teaSink struct {
	prog   *tea.Program
	live   *liveState
	scene  *SceneRenderer
	pool   *agent.AgentPool
	cmds   *Registry
	mainID string
}

// compile-time proof the TUI sink satisfies the renderer-neutral contract.
var _ sdk.UISink = (*teaSink)(nil)

func (s *teaSink) send(msg tea.Msg) {
	if s.prog != nil {
		s.prog.Send(msg)
	}
}

func (s *teaSink) Notify(text string) {
	s.send(NotifyMsg{Text: text})
}

func (s *teaSink) ShowModal(text string) {
	s.send(ShowModalMsg{Text: text})
}

func (s *teaSink) ShowPicker(params sdk.ShowPickerParams) {
	s.send(ShowPickerMsg{Title: params.Title, Items: params.Items, Callback: params.Callback, Split: params.Split})
}

func (s *teaSink) ShowAgentTree(params sdk.ShowAgentTreeParams) {
	s.send(ShowAgentTreeMsg{Title: params.Title, Callback: params.Callback, Nodes: params.Nodes})
}

func (s *teaSink) FocusAgent(id string) {
	s.send(FocusAgentMsg{AgentID: id})
}

func (s *teaSink) ShowTextInput(title, placeholder, initialValue, callback string) {
	s.send(ShowTextInputMsg{Title: title, Placeholder: placeholder, InitialValue: initialValue, Callback: callback})
}

func (s *teaSink) Abort() {
	s.send(abortStreamMsg{})
}

func (s *teaSink) SetStatus(key, value string) {
	s.send(StatusUpdateMsg{Key: key, Value: value})
}

// StatusInfo returns the status snapshot the statusline extension reads. A
// focused sub-agent can vanish without the focusing extension being told (an
// agent self-closes once it has processed its shutdown request); reporting a
// dead id would leave the statusline naming an agent that no longer exists, so
// the key is dropped and focus falls back to the root.
func (s *teaSink) StatusInfo() sdk.StatusInfo {
	live := s.live
	if live == nil {
		return sdk.StatusInfo{Statuses: map[string]string{}}
	}
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

	if id := statuses["agent"]; id != "" && s.pool != nil && s.pool.Get(id) == nil {
		delete(statuses, "agent")
	}

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
	if s.pool != nil {
		n := len(s.pool.ListAgents()) - 1
		if n < 0 {
			n = 0
		}
		info.ActiveAgents = n
	}
	return info
}

func (s *teaSink) Submit(content, display string) {
	sm := SubmitMsg{Content: content}
	if display != "" {
		sm.Display = display
	} else if strings.HasPrefix(strings.TrimSpace(content), "<skill ") {
		sm.Display = skillDisplayName(content)
	}
	s.send(sm)
}

func (s *teaSink) SetModel(value, thinking string) error {
	// A thinking-only request still goes through setModelMsg with an empty
	// model; applyModelSelection treats an empty model as a no-op, so only the
	// thinking level changes.
	s.send(setModelMsg{Model: value, Thinking: thinking})
	return nil
}

func (s *teaSink) RegisterCommand(name, desc string, instant bool) error {
	if s.cmds == nil {
		return nil
	}
	s.cmds.Register(Command{
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

func (s *teaSink) Console(line string, clear bool) {
	if clear {
		s.send(ConsoleMsg{Clear: true})
		return
	}
	s.send(ConsoleMsg{Line: line})
}

func (s *teaSink) ResetHistory(messages []sdk.Message) {
	s.send(ResetHistoryMsg{Messages: messages})
}

func (s *teaSink) Token(text string) {
	s.send(TokenMsg{Token: text})
}

func (s *teaSink) ToolCallStart(agentID, id, toolName, input string) {
	s.send(ToolCallStartMsg{AgentID: agentID, ID: id, ToolName: toolName, Input: input})
}

func (s *teaSink) ToolCallDone(agentID, id, toolName, output string, isError bool) {
	s.send(ToolCallDoneMsg{AgentID: agentID, ID: id, ToolName: toolName, IsError: isError, Output: output})
}

func (s *teaSink) StreamDone(err error) {
	s.send(StreamDoneMsg{Err: err})
}

// AgentWakeup is only meaningful for the root agent: sub-agents do not have TUI
// streaming state. A non-root id is ignored.
func (s *teaSink) AgentWakeup(agentID string) {
	if agentID != "" && agentID != s.mainID {
		return
	}
	s.send(agentWakeupMsg{})
}

func (s *teaSink) SceneCreate(area sdk.UIArea) error {
	if s.scene == nil {
		return fmt.Errorf("ui: scene renderer not available")
	}
	if err := s.scene.CreateArea(area); err != nil {
		return err
	}
	s.send(sceneDirtyMsg{Area: area.ID})
	return nil
}

func (s *teaSink) ScenePatch(params sdk.UIPatchParams) error {
	if s.scene == nil {
		return fmt.Errorf("ui: scene renderer not available")
	}
	if err := s.scene.ApplyPatch(params); err != nil {
		return err
	}
	appendID, appendText, appendOnly := appendPatchDetails(params)
	s.send(sceneDirtyMsg{Area: params.Area, AppendOnly: appendOnly, AppendID: appendID, AppendText: appendText})
	return nil
}

func (s *teaSink) SceneUpdate(params sdk.UIUpdateAreaParams) error {
	if s.scene == nil {
		return fmt.Errorf("ui: scene renderer not available")
	}
	if err := s.scene.UpdateArea(params); err != nil {
		return err
	}
	s.send(sceneDirtyMsg{Area: params.ID})
	return nil
}

func (s *teaSink) SceneRemove(id string) {
	if s.scene == nil {
		return
	}
	s.scene.RemoveArea(id)
	s.send(sceneDirtyMsg{Area: id})
}

// appendPatchDetails reports whether a patch is a pure append of contiguous
// text to one node, so the TUI can append incrementally instead of rebuilding
// the transcript viewport.
func appendPatchDetails(params sdk.UIPatchParams) (string, string, bool) {
	if len(params.Ops) == 0 {
		return "", "", false
	}
	id := params.Ops[0].ID
	var text strings.Builder
	for _, op := range params.Ops {
		if op.Op != sdk.UIOpAppendText || op.ID == "" || op.ID != id {
			return "", "", false
		}
		text.WriteString(op.Text)
	}
	return id, text.String(), true
}
