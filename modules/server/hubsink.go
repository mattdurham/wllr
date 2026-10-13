package server

// NOTE: Any changes to this file must be reflected in the corresponding SPECS.md or NOTES.md.

import (
	"strings"
	"sync"

	"github.com/mattdurham/wllr/modules/agent"
	"github.com/mattdurham/wllr/modules/sdk"
)

// HubSink is the HTTP/SSE ("HTML") implementation of the renderer-neutral
// sdk.UISink. It is the non-terminal counterpart of the harness's teaSink:
// where the TUI turns each effect into a bubbletea message, HubSink publishes a
// stream frame so a browser client can render the same UI the TUI would have.
//
// It carries no engine logic. The engine-local mutations an extension can
// request (base system prompt, history reset, tool-call bookkeeping) are
// performed by the harness's UIBridge, which then forwards the user-visible
// effect here. Serve mode therefore installs the *same* harness bridges the TUI
// uses, differing only in the sink — which is what lets a browser front end
// drive the engine (and spawn sub-agents) without a terminal.
//
// Every method is safe for concurrent use: UISink calls arrive from concurrent
// WASM dispatch goroutines. Publishing is non-blocking (see Hub), so a slow
// client can never stall an extension call.
type HubSink struct {
	pool     *agent.AgentPool
	hub      *Hub
	scene    *SceneModel
	statuses map[string]string
	// registerCmd receives commands an extension registers after serve-mode
	// wiring. Serve mode points it at the harness command registry, so a command
	// registered here appears in GET /api/commands alongside the built-ins
	// instead of being dropped.
	registerCmd func(name, desc string, instant bool)
	mainID      string

	focus string

	mu sync.Mutex
}

// NewHubSink returns a HubSink publishing frames to hub. mainID is the root
// agent's ID; it is the default target for Submit and the default owner of
// status and transcript state.
func NewHubSink(pool *agent.AgentPool, hub *Hub, mainID string) *HubSink {
	if mainID == "" {
		mainID = agent.MainAgentID
	}
	return &HubSink{
		pool:     pool,
		hub:      hub,
		scene:    NewSceneModel(),
		mainID:   mainID,
		statuses: map[string]string{},
	}
}

// compile-time proof HubSink satisfies the renderer-neutral sink contract.
var _ sdk.UISink = (*HubSink)(nil)

// SetCommandRegistrar installs the receiver for commands registered through
// RegisterCommand. Serve mode points it at the harness registry so the browser's
// command list stays complete.
func (b *HubSink) SetCommandRegistrar(fn func(name, desc string, instant bool)) {
	b.mu.Lock()
	b.registerCmd = fn
	b.mu.Unlock()
}

// Notify publishes a human-readable notice to the stream.
func (b *HubSink) Notify(text string) {
	b.hub.Publish(Event{Kind: KindNotify, AgentID: b.mainID, Text: text})
}

// ShowModal publishes a modal overlay carrying text.
func (b *HubSink) ShowModal(text string) {
	b.hub.Publish(Event{Kind: KindModal, AgentID: b.mainID, Text: text})
}

// ShowPicker publishes an interactive picker overlay. The Callback is echoed so
// a client can route the selection back.
func (b *HubSink) ShowPicker(params sdk.ShowPickerParams) {
	p := params
	b.hub.Publish(Event{Kind: KindPicker, AgentID: b.mainID, Picker: &p})
}

// ShowAgentTree publishes an agent-tree overlay.
func (b *HubSink) ShowAgentTree(params sdk.ShowAgentTreeParams) {
	p := params
	b.hub.Publish(Event{Kind: KindAgentTree, AgentID: b.mainID, AgentTree: &p})
}

// FocusAgent records and publishes the focused agent. An empty id means root.
func (b *HubSink) FocusAgent(id string) {
	b.mu.Lock()
	b.focus = id
	b.mu.Unlock()
	b.hub.Publish(Event{Kind: KindFocus, AgentID: b.mainID, Text: id})
}

// ShowTextInput publishes a free-form text prompt overlay.
func (b *HubSink) ShowTextInput(title, placeholder, initialValue, callback string) {
	b.hub.Publish(Event{
		Kind:    KindTextInput,
		AgentID: b.mainID,
		TextInput: &TextInputParams{
			Title:        title,
			Placeholder:  placeholder,
			InitialValue: initialValue,
			Callback:     callback,
		},
	})
}

// Abort publishes a request to stop showing an in-progress stream. It does not
// cancel the turn; cancellation is the explicit /api/cancel operation.
func (b *HubSink) Abort() {
	b.hub.Publish(Event{Kind: KindAbort, AgentID: b.mainID})
}

// SetStatus records and publishes a status-line key/value pair.
func (b *HubSink) SetStatus(key, value string) {
	b.mu.Lock()
	if b.statuses == nil {
		b.statuses = map[string]string{}
	}
	b.statuses[key] = value
	b.mu.Unlock()
	b.hub.Publish(Event{Kind: KindStatus, AgentID: b.mainID, Key: key, Value: value})
}

// StatusInfo returns the accumulated status map plus provider/model/agent
// counts derived from the pool, matching the fields the statusline reads. Width
// and elapsed-time fields are terminal concerns and stay zero.
func (b *HubSink) StatusInfo() sdk.StatusInfo {
	b.mu.Lock()
	statuses := make(map[string]string, len(b.statuses))
	for k, v := range b.statuses {
		statuses[k] = v
	}
	b.mu.Unlock()

	info := sdk.StatusInfo{Statuses: statuses}
	if b.pool != nil {
		info.Provider = b.pool.ProviderName()
		if main := b.pool.Get(b.mainID); main != nil {
			info.Model = main.ModelName()
		}
		if n := len(b.pool.ListAgents()) - 1; n > 0 {
			info.ActiveAgents = n
		}
	}
	return info
}

// Submit injects content as a user turn on the focused agent (defaulting to the
// root). The TUI maps this to its submit path; here it maps to the pool's Send.
// A send failure (for example an extension targeting an agent that has vanished)
// is published as an error frame, since the sink has no return value to surface
// it — the HTTP submit path reports the same failure as a 500.
func (b *HubSink) Submit(content, _ string) {
	if b.pool == nil || strings.TrimSpace(content) == "" {
		return
	}
	if err := b.pool.Send(b.focusAgent(), content); err != nil {
		b.hub.Publish(Event{Kind: KindError, AgentID: b.mainID, Text: err.Error()})
	}
}

// SetModel is a no-op in serve mode: model/tier switching in the TUI runs
// through the harness's setModelMsg handler, which resolves tiers and may
// rebuild the provider. Returning nil keeps the contract honest — the request
// was accepted but nothing changed.
func (b *HubSink) SetModel(_, _ string) error { return nil }

// RegisterCommand records an extension-registered slash command. Serve mode has
// no slash-command dispatch loop of its own, but the command must still appear
// in the browser's autocomplete and help, so it is forwarded to the harness
// registry (installed via SetCommandRegistrar). It returns nil: registration is
// side-effect only from the extension's point of view, exactly as in the TUI.
func (b *HubSink) RegisterCommand(name, desc string, instant bool) error {
	b.mu.Lock()
	fn := b.registerCmd
	b.mu.Unlock()
	if fn != nil {
		fn(name, desc, instant)
	}
	return nil
}

// Console appends a line to the console pane or clears it.
func (b *HubSink) Console(line string, clear bool) {
	if clear {
		b.hub.Publish(Event{Kind: KindConsoleClear, AgentID: b.mainID})
		return
	}
	b.hub.Publish(Event{Kind: KindConsole, AgentID: b.mainID, Text: line})
}

// ResetHistory publishes the replacement transcript. The engine-side history
// write is performed by the harness UIBridge before this is called.
func (b *HubSink) ResetHistory(messages []sdk.Message) {
	b.hub.Publish(Event{Kind: KindResetHistory, AgentID: b.mainID, Messages: messages})
}

// Token publishes one streamed text delta for the root transcript. In serve
// mode the per-agent observers in Bridge normally deliver tokens, so this path
// is only used when the harness main-agent callbacks are wired to the sink.
func (b *HubSink) Token(text string) {
	b.hub.Publish(Event{Kind: KindToken, AgentID: b.mainID, Text: text})
}

// ToolCallStart publishes a tool dispatch.
func (b *HubSink) ToolCallStart(agentID, id, toolName, input string) {
	if agentID == "" {
		agentID = b.mainID
	}
	b.hub.Publish(Event{
		Kind:       KindToolCall,
		AgentID:    agentID,
		ToolCallID: id,
		ToolName:   toolName,
		Input:      input,
	})
}

// ToolCallDone publishes a tool completion with its output.
func (b *HubSink) ToolCallDone(agentID, id, toolName, output string, isError bool) {
	if agentID == "" {
		agentID = b.mainID
	}
	b.hub.Publish(Event{
		Kind:       KindToolResult,
		AgentID:    agentID,
		ToolCallID: id,
		ToolName:   toolName,
		Output:     output,
		IsError:    isError,
	})
}

// StreamDone publishes a turn completion. As with Token, serve mode normally
// reports turn completion through Bridge's per-agent onDone observer.
func (b *HubSink) StreamDone(err error) {
	ev := Event{Kind: KindTurnDone, AgentID: b.mainID}
	if err != nil {
		ev.Err = err.Error()
	}
	b.hub.Publish(ev)
}

// AgentWakeup is a no-op: the "working" indicator in serve mode is derived from
// the turn_started events Bridge already publishes, so emitting another would
// duplicate it.
func (b *HubSink) AgentWakeup(string) {}

// SceneModel exposes the scene graph holder so a client can fetch a full
// snapshot (GET /api/scene) to seed its view before applying live frames.
func (b *HubSink) SceneModel() *SceneModel { return b.scene }

// SceneCreate records an area and publishes both its registration and its
// (empty) rendered HTML. An extension that only creates an area and populates it
// with a later patch still gets a visible region immediately.
func (b *HubSink) SceneCreate(area sdk.UIArea) error {
	if err := b.scene.CreateArea(area); err != nil {
		return err
	}
	a := area
	html, _ := b.scene.AreaHTML(area.ID)
	b.hub.Publish(Event{Kind: KindUIArea, AgentID: b.mainID, Area: &a})
	b.hub.Publish(Event{Kind: KindUIAreaHTML, AgentID: b.mainID, Area: &a, HTML: html})
	return nil
}

// ScenePatch applies a batch of ops and publishes the result. A batch that is
// purely append_text takes the incremental fast path (one lightweight text
// delta per op); any structural change re-renders the whole area. The fast path
// mirrors the terminal renderer's append optimisation so streaming a long
// message does not re-send the entire transcript per token.
func (b *HubSink) ScenePatch(params sdk.UIPatchParams) error {
	if err := b.scene.ApplyPatch(params); err != nil {
		return err
	}
	p := params
	b.hub.Publish(Event{Kind: KindUIPatch, AgentID: b.mainID, Patch: &p})
	if IsTextAppendOnly(params.Ops) {
		for _, op := range params.Ops {
			b.hub.Publish(Event{
				Kind:    KindUITextAppend,
				AgentID: b.mainID,
				AreaID:  params.Area,
				NodeID:  op.ID,
				Text:    op.Text,
			})
		}
		return nil
	}
	b.publishAreaHTML(params.Area)
	return nil
}

// SceneUpdate applies a sizing/weight change and re-publishes the area HTML so
// the client's region picks up the new constraints.
func (b *HubSink) SceneUpdate(params sdk.UIUpdateAreaParams) error {
	if err := b.scene.UpdateArea(params); err != nil {
		return err
	}
	p := params
	b.hub.Publish(Event{Kind: KindUIAreaUpdate, AgentID: b.mainID, AreaUpdate: &p})
	b.publishAreaHTML(params.ID)
	return nil
}

// SceneRemove drops an area and tells the client to remove its region.
func (b *HubSink) SceneRemove(id string) {
	b.scene.RemoveArea(id)
	b.hub.Publish(Event{Kind: KindUIAreaRemove, AgentID: b.mainID, Text: id})
}

// publishAreaHTML renders one area and publishes it as a full-HTML frame. It is
// a silent no-op for an unknown area (a race between a structural patch and a
// removal), so a client never receives HTML for a region it has dropped.
func (b *HubSink) publishAreaHTML(areaID string) {
	html, ok := b.scene.AreaHTML(areaID)
	if !ok {
		return
	}
	b.hub.Publish(Event{Kind: KindUIAreaHTML, AgentID: b.mainID, AreaID: areaID, HTML: html})
}

// focusAgent returns the focused agent ID, defaulting to the root agent.
func (b *HubSink) focusAgent() string {
	b.mu.Lock()
	defer b.mu.Unlock()
	if b.focus == "" {
		return b.mainID
	}
	return b.focus
}
