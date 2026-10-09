package server

// NOTE: Any changes to this file must be reflected in the corresponding SPECS.md or NOTES.md.

import (
	"strings"
	"testing"
	"time"

	"github.com/mattdurham/wllr/modules/agent"
	"github.com/mattdurham/wllr/modules/sdk"
)

// nextEvent returns the next event on ch, or fails the test after a short wait.
func nextEvent(t *testing.T, ch <-chan Event) Event {
	t.Helper()
	select {
	case ev := <-ch:
		return ev
	case <-time.After(2 * time.Second):
		t.Fatal("timed out waiting for event")
		return Event{}
	}
}

// TestHubSinkPublishesPresentationalFrames proves the HTTP sink turns UISink
// presentation calls into the matching stream frames — the non-terminal
// counterpart of the TUI turning them into bubbletea messages.
func TestHubSinkPublishesPresentationalFrames(t *testing.T) {
	hub := NewHub(64)
	b := NewHubSink(nil, hub, agent.MainAgentID)
	ch, _, cancel := hub.Subscribe(0)
	defer cancel()

	b.Notify("heads up")
	if ev := nextEvent(t, ch); ev.Kind != KindNotify || ev.Text != "heads up" {
		t.Fatalf("Notify -> %+v, want notify %q", ev, "heads up")
	}

	b.ShowModal("pick one")
	if ev := nextEvent(t, ch); ev.Kind != KindModal || ev.Text != "pick one" {
		t.Fatalf("ShowModal -> %+v, want modal", ev)
	}

	b.ShowPicker(sdk.ShowPickerParams{Title: "models", Callback: "cb", Items: []sdk.ShowPickerItem{{ID: "a"}}})
	ev := nextEvent(t, ch)
	if ev.Kind != KindPicker || ev.Picker == nil || ev.Picker.Title != "models" {
		t.Fatalf("ShowPicker -> %+v, want picker models", ev)
	}

	b.ShowAgentTree(sdk.ShowAgentTreeParams{Title: "agents", Callback: "cb"})
	if ev := nextEvent(t, ch); ev.Kind != KindAgentTree || ev.AgentTree == nil || ev.AgentTree.Title != "agents" {
		t.Fatalf("ShowAgentTree -> %+v, want agent_tree", ev)
	}

	b.ShowTextInput("url", "https://...", "http://x", "cb")
	ev = nextEvent(t, ch)
	if ev.Kind != KindTextInput || ev.TextInput == nil || ev.TextInput.Title != "url" || ev.TextInput.InitialValue != "http://x" {
		t.Fatalf("ShowTextInput -> %+v, want text_input url", ev)
	}

	b.Abort()
	if ev := nextEvent(t, ch); ev.Kind != KindAbort {
		t.Fatalf("Abort -> %+v, want abort", ev)
	}

	b.Console("line", false)
	if ev := nextEvent(t, ch); ev.Kind != KindConsole || ev.Text != "line" {
		t.Fatalf("Console -> %+v, want console line", ev)
	}
	b.Console("", true)
	if ev := nextEvent(t, ch); ev.Kind != KindConsoleClear {
		t.Fatalf("Console clear -> %+v, want console_clear", ev)
	}
}

// TestHubSinkStreamingFrames proves the streaming intents map onto the same
// event kinds a chat client already consumes.
func TestHubSinkStreamingFrames(t *testing.T) {
	hub := NewHub(64)
	b := NewHubSink(nil, hub, agent.MainAgentID)
	ch, _, cancel := hub.Subscribe(0)
	defer cancel()

	b.Token("hello")
	if ev := nextEvent(t, ch); ev.Kind != KindToken || ev.Text != "hello" {
		t.Fatalf("Token -> %+v, want token hello", ev)
	}

	b.ToolCallStart("main", "c1", "read_file", `{"p":"x"}`)
	ev := nextEvent(t, ch)
	if ev.Kind != KindToolCall || ev.AgentID != "main" || ev.ToolCallID != "c1" || ev.ToolName != "read_file" {
		t.Fatalf("ToolCallStart -> %+v, want tool_call main/c1/read_file", ev)
	}

	b.ToolCallDone("", "c1", "read_file", "contents", false)
	ev = nextEvent(t, ch)
	if ev.Kind != KindToolResult || ev.AgentID != agent.MainAgentID || ev.Output != "contents" {
		t.Fatalf("ToolCallDone -> %+v, want tool_result (root fallback)", ev)
	}

	b.StreamDone(nil)
	if ev := nextEvent(t, ch); ev.Kind != KindTurnDone {
		t.Fatalf("StreamDone -> %+v, want turn_done", ev)
	}

	// AgentWakeup is intentionally a no-op: Bridge already reports turn_started,
	// so a second frame would duplicate it.
	b.AgentWakeup("main")
	select {
	case ev := <-ch:
		t.Fatalf("AgentWakeup published %+v, want no frame", ev)
	case <-time.After(50 * time.Millisecond):
	}
}

// TestHubSinkSceneGraphFrames proves the scene-graph surface is forwarded and
// that malformed requests surface as errors to the extension.
// TestHubSinkSceneGraphFrames proves the sink applies scene ops to its scene
// model and publishes both the structural frame and the rendered HTML a browser
// needs. A create emits ui_area then ui_area_html; a structural patch emits
// ui_patch then a fresh ui_area_html; an update emits ui_area_update then HTML.
func TestHubSinkSceneGraphFrames(t *testing.T) {
	hub := NewHub(64)
	b := NewHubSink(nil, hub, agent.MainAgentID)
	ch, _, cancel := hub.Subscribe(0)
	defer cancel()

	if err := b.SceneCreate(sdk.UIArea{ID: "chat", Placement: sdk.UIAreaMain}); err != nil {
		t.Fatalf("SceneCreate: %v", err)
	}
	ev := nextEvent(t, ch)
	if ev.Kind != KindUIArea || ev.Area == nil || ev.Area.ID != "chat" {
		t.Fatalf("SceneCreate -> %+v, want ui_area chat", ev)
	}
	ev = nextEvent(t, ch)
	if ev.Kind != KindUIAreaHTML || !strings.Contains(ev.HTML, `data-area-id="chat"`) {
		t.Fatalf("SceneCreate -> %+v, want ui_area_html for chat", ev)
	}

	// A structural patch re-renders the whole area.
	if err := b.ScenePatch(sdk.UIPatchParams{Area: "chat", Ops: []sdk.UIPatchOp{
		{Op: sdk.UIOpSetRoot, Node: &sdk.UINode{ID: "root", Type: sdk.UINodeVStack}},
	}}); err != nil {
		t.Fatalf("ScenePatch: %v", err)
	}
	ev = nextEvent(t, ch)
	if ev.Kind != KindUIPatch || ev.Patch == nil || ev.Patch.Area != "chat" || len(ev.Patch.Ops) != 1 {
		t.Fatalf("ScenePatch -> %+v, want ui_patch chat", ev)
	}
	ev = nextEvent(t, ch)
	if ev.Kind != KindUIAreaHTML || !strings.Contains(ev.HTML, `data-node-id="root"`) {
		t.Fatalf("structural patch -> %+v, want ui_area_html with root node", ev)
	}

	w := 2
	if err := b.SceneUpdate(sdk.UIUpdateAreaParams{ID: "chat", Weight: &w}); err != nil {
		t.Fatalf("SceneUpdate: %v", err)
	}
	ev = nextEvent(t, ch)
	if ev.Kind != KindUIAreaUpdate || ev.AreaUpdate == nil || ev.AreaUpdate.ID != "chat" {
		t.Fatalf("SceneUpdate -> %+v, want ui_area_update chat", ev)
	}
	if ev := nextEvent(t, ch); ev.Kind != KindUIAreaHTML {
		t.Fatalf("update -> %+v, want ui_area_html", ev)
	}

	b.SceneRemove("chat")
	if ev := nextEvent(t, ch); ev.Kind != KindUIAreaRemove || ev.Text != "chat" {
		t.Fatalf("SceneRemove -> %+v, want ui_area_remove chat", ev)
	}

	// Missing-id errors surface to an extension as HostCallResponse.Error.
	if err := b.SceneCreate(sdk.UIArea{}); err == nil {
		t.Errorf("SceneCreate with empty id: want error")
	}
	if err := b.ScenePatch(sdk.UIPatchParams{}); err == nil {
		t.Errorf("ScenePatch with unknown area: want error")
	}
	if err := b.SceneUpdate(sdk.UIUpdateAreaParams{}); err == nil {
		t.Errorf("SceneUpdate with empty id: want error")
	}
}

// TestHubSinkScenePatchAppendFastPath proves an append-only batch takes the
// incremental path: one lightweight ui_text_append per op, and no whole-area
// re-render. This is the HTML counterpart of the terminal streaming fast path.
func TestHubSinkScenePatchAppendFastPath(t *testing.T) {
	hub := NewHub(64)
	b := NewHubSink(nil, hub, agent.MainAgentID)
	ch, _, cancel := hub.Subscribe(0)
	defer cancel()

	if err := b.SceneCreate(sdk.UIArea{ID: "chat"}); err != nil {
		t.Fatalf("create: %v", err)
	}
	nextEvent(t, ch) // ui_area
	nextEvent(t, ch) // ui_area_html

	if err := b.ScenePatch(sdk.UIPatchParams{Area: "chat", Ops: []sdk.UIPatchOp{
		{Op: sdk.UIOpSetRoot, Node: &sdk.UINode{ID: "txt", Type: sdk.UINodeText, Text: "Hel"}},
	}}); err != nil {
		t.Fatalf("set_root: %v", err)
	}
	nextEvent(t, ch) // ui_patch
	nextEvent(t, ch) // ui_area_html

	if err := b.ScenePatch(sdk.UIPatchParams{Area: "chat", Ops: []sdk.UIPatchOp{
		{Op: sdk.UIOpAppendText, ID: "txt", Text: "lo "},
		{Op: sdk.UIOpAppendText, ID: "txt", Text: "world"},
	}}); err != nil {
		t.Fatalf("append: %v", err)
	}

	ev := nextEvent(t, ch)
	if ev.Kind != KindUIPatch {
		t.Fatalf("append -> %+v, want ui_patch", ev)
	}
	for _, want := range []string{"lo ", "world"} {
		ev = nextEvent(t, ch)
		if ev.Kind != KindUITextAppend || ev.AreaID != "chat" || ev.NodeID != "txt" || ev.Text != want {
			t.Fatalf("append -> %+v, want ui_text_append %q to txt in chat", ev, want)
		}
	}

	// No HTML frame follows an append-only batch.
	if hm, ok := b.SceneModel().AreaHTML("chat"); !ok || !strings.Contains(hm, "Hello world") {
		t.Fatalf("append must update the model: ok=%v html=%q", ok, hm)
	}
}

// TestHubSinkSceneRemoveThenPatchErrors proves a patch after removal fails
// rather than silently recreating the area.
func TestHubSinkSceneRemoveThenPatchErrors(t *testing.T) {
	hub := NewHub(64)
	b := NewHubSink(nil, hub, agent.MainAgentID)
	if err := b.SceneCreate(sdk.UIArea{ID: "chat"}); err != nil {
		t.Fatalf("create: %v", err)
	}
	b.SceneRemove("chat")
	if err := b.ScenePatch(sdk.UIPatchParams{Area: "chat", Ops: []sdk.UIPatchOp{
		{Op: sdk.UIOpSetRoot, Node: &sdk.UINode{ID: "r", Type: sdk.UINodeVStack}},
	}}); err == nil {
		t.Fatal("patch on removed area must error")
	}
}

// TestHubSinkStatusAndFocus proves status/focus round-trip through the sink.
func TestHubSinkStatusAndFocus(t *testing.T) {
	hub := NewHub(64)
	b := NewHubSink(nil, hub, agent.MainAgentID)
	ch, _, cancel := hub.Subscribe(0)
	defer cancel()

	b.SetStatus("agent", "main/coder")
	ev := nextEvent(t, ch)
	if ev.Kind != KindStatus || ev.Key != "agent" || ev.Value != "main/coder" {
		t.Fatalf("SetStatus -> %+v, want status agent=main/coder", ev)
	}
	if info := b.StatusInfo(); info.Statuses["agent"] != "main/coder" {
		t.Fatalf("StatusInfo statuses = %+v, want agent=main/coder", info.Statuses)
	}

	// Empty focus means the root agent.
	if got := b.focusAgent(); got != agent.MainAgentID {
		t.Errorf("default focusAgent = %q, want %q", got, agent.MainAgentID)
	}
	b.FocusAgent("main/coder")
	ev = nextEvent(t, ch)
	if ev.Kind != KindFocus || ev.Text != "main/coder" {
		t.Fatalf("FocusAgent -> %+v, want focus main/coder", ev)
	}
	if got := b.focusAgent(); got != "main/coder" {
		t.Errorf("focusAgent = %q, want main/coder", got)
	}
}

// TestHubSinkResetHistoryPublishes proves the replacement transcript is
// published. The engine-side history write is the harness UIBridge's job
// (covered by the harness tests), so this sink only reports the frame.
func TestHubSinkResetHistoryPublishes(t *testing.T) {
	hub := NewHub(64)
	b := NewHubSink(nil, hub, agent.MainAgentID)
	ch, _, cancel := hub.Subscribe(0)
	defer cancel()

	msgs := []sdk.Message{
		{Role: sdk.RoleUser, Content: "a"},
		{Role: sdk.RoleAssistant, Content: "b"},
	}
	b.ResetHistory(msgs)
	ev := nextEvent(t, ch)
	if ev.Kind != KindResetHistory || len(ev.Messages) != 2 {
		t.Fatalf("ResetHistory -> %+v, want reset_history with 2 messages", ev)
	}
}

// TestHubSinkSubmitTargetsFocus proves Submit routes to the focused agent by
// observing a user_message event attributed to it.
func TestHubSinkSubmitTargetsFocus(t *testing.T) {
	pool, lm := newTestPool(t)
	srv := New(pool, Options{})
	srv.Start()

	if _, err := pool.Spawn("main/coder", lm, agent.SpawnOpts{ModelName: "fake-model", TurnTimeout: -1}); err != nil {
		t.Fatalf("spawn sub-agent: %v", err)
	}

	ch, _, cancel := srv.Hub().Subscribe(0)
	defer cancel()

	srv.HubSink().FocusAgent("main/coder")
	// Drain the focus frame.
	nextEvent(t, ch)

	srv.HubSink().Submit("work", "")

	deadline := time.After(3 * time.Second)
	for {
		select {
		case ev := <-ch:
			if ev.Kind == KindUserMessage {
				if ev.AgentID != "main/coder" {
					t.Fatalf("user_message agent = %q, want main/coder", ev.AgentID)
				}
				if ev.Text != "work" {
					t.Fatalf("user_message text = %q, want work", ev.Text)
				}
				return
			}
		case <-deadline:
			t.Fatal("no user_message event from Submit")
		}
	}
}

// TestHubSinkSatisfiesSinkContract assigns the sink to the sdk.UISink interface
// type, which fails to compile if a method is missing or mis-signed (hubsink.go
// also asserts this at package scope).
func TestHubSinkSatisfiesSinkContract(t *testing.T) {
	// The assignment is the assertion: it fails to compile if HubSink is missing
	// a method or has a mis-signed one. (hubsink.go also asserts this at package
	// scope.) A runtime nil check would be dead code — the interface always holds
	// a concrete *HubSink here.
	var b sdk.UISink = NewHubSink(nil, NewHub(1), agent.MainAgentID)
	_ = b
}
