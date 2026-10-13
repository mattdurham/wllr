package server

// NOTE: Any changes to this file must be reflected in the corresponding SPECS.md or NOTES.md.

import (
	"fmt"
	"strings"
	"sync"

	"github.com/mattdurham/wllr/modules/agent"
	"github.com/mattdurham/wllr/modules/sdk"
)

// Bridge translates the engine's callback surface into hub events.
//
// The engine exposes two kinds of hook. Per-agent hooks (SetOnToken,
// SetOnToolCall, SetOnTurnStart, SetOnDone) are installed on each *agent.Agent,
// so the Bridge attaches them whenever an agent appears — using the pool's
// lifecycle observer to notice spawns after Start, and a sweep of existing
// agents at Start. Pool-level hooks (usage, lifecycle, context usage) are
// shared, so the Bridge uses the additive AddUsageObserver/AddLifecycleObserver
// forms to coexist with the metrics recorder, and leaves the single-slot
// context-usage dispatcher to be wired explicitly by the caller in serve mode
// (HandleContextUsage).
//
// Every method is safe for concurrent use and never blocks: it publishes to the
// Hub, whose Publish is non-blocking.
type Bridge struct {
	pool *agent.AgentPool
	hub  *Hub

	// messageEnd is invoked when a turn's assistant text is finalized, with the
	// accumulated content. Nil is a no-op. See SetMessageEndNotifier.
	messageEnd func(role, content string)
	mainID     string
}

// SetMessageEndNotifier installs the turn-finalization callback. It must be set
// before Start so every attached agent sees it.
func (b *Bridge) SetMessageEndNotifier(fn func(role, content string)) {
	b.messageEnd = fn
}

// NewBridge returns a Bridge publishing to hub for the agents of pool. mainID
// is the root agent's ID (normally agent.MainAgentID); it is reported on events
// so a client can distinguish the root from sub-agents.
func NewBridge(pool *agent.AgentPool, hub *Hub, mainID string) *Bridge {
	if mainID == "" {
		mainID = agent.MainAgentID
	}
	return &Bridge{pool: pool, hub: hub, mainID: mainID}
}

// Start installs the pool-level observers, emits membership for agents that
// already exist, and attaches per-agent observers to each. Calling Start more
// than once would double-subscribe; callers should call it exactly once.
func (b *Bridge) Start() {
	if b.pool == nil {
		return
	}
	b.pool.AddLifecycleObserver(b.onLifecycle)
	b.pool.AddUsageObserver(b.onUsage)
	for _, id := range b.pool.ListAgents() {
		b.attach(id)
		b.emitMembership(id, true, int64(len(b.pool.ListAgents())))
	}
}

// onLifecycle reacts to a pool membership change: attach observers to a new
// agent and broadcast the change.
func (b *Bridge) onLifecycle(ev agent.AgentLifecycle) {
	if ev.Spawned {
		b.attach(ev.AgentID)
	}
	kind := KindAgentClosed
	if ev.Spawned {
		kind = KindAgentSpawned
	}
	name := ""
	if a := b.pool.Get(ev.AgentID); a != nil {
		name = a.Name()
	}
	b.hub.Publish(Event{
		Kind:    kind,
		AgentID: ev.AgentID,
		Name:    name,
		Live:    ev.Live,
		Main:    ev.Main,
	})
}

// emitMembership publishes a spawn/close event for a known agent (used for the
// initial sweep, where the pool does not fire a lifecycle callback).
func (b *Bridge) emitMembership(id string, spawned bool, live int64) {
	kind := KindAgentClosed
	if spawned {
		kind = KindAgentSpawned
	}
	name := ""
	if a := b.pool.Get(id); a != nil {
		name = a.Name()
	}
	b.hub.Publish(Event{
		Kind:    kind,
		AgentID: id,
		Name:    name,
		Live:    live,
		Main:    id == b.mainID,
	})
}

// attach installs the per-agent stream observers on agentID. A missing agent is
// ignored (it may have been closed in the same tick a spawn was reported).
//
// The observers accumulate the turn's streamed text per agent (guarded by a
// closure-local mutex, since tokens arrive from the agent's streaming goroutine)
// so the turn can be finalized with a rendered message. The buffer is reset at
// turn start, so a failed or empty turn cannot leak text into the next one.
func (b *Bridge) attach(id string) {
	a := b.pool.Get(id)
	if a == nil {
		return
	}
	var bufMu sync.Mutex
	var buf strings.Builder

	// Observers are registered ADDITIVELY (Add*): the Bridge is one of possibly
	// several consumers of an agent's stream — the harness also wires the root
	// agent's callbacks (Model.WireMainAgentCallbacks) — and Set* would silently
	// replace them. With Set* the last subscriber won and the others were
	// dropped, which is exactly how the browser lost its streamed transcript and
	// its user-prompt bubbles.
	a.AddOnTurnStart(func(content string, inbox []sdk.Message) {
		bufMu.Lock()
		buf.Reset()
		bufMu.Unlock()
		b.hub.Publish(Event{
			Kind:    KindUserMessage,
			AgentID: id,
			Role:    sdk.RoleUser,
			Text:    content,
			Inbox:   inbox,
		})
	})
	a.AddOnToken(func(tok string) {
		bufMu.Lock()
		buf.WriteString(tok)
		bufMu.Unlock()
		b.hub.Publish(Event{Kind: KindToken, AgentID: id, Text: tok})
	})
	a.AddOnToolCall(func(callID, toolName, input string) {
		b.hub.Publish(Event{
			Kind:       KindToolCall,
			AgentID:    id,
			ToolCallID: callID,
			ToolName:   toolName,
			Input:      input,
		})
	})
	a.AddOnDone(func(err error) {
		// Finalize the streamed text before signaling completion: the client
		// replaces the in-flight bubble on this frame, then clears its streaming
		// state on turn_done, so the render must arrive first.
		bufMu.Lock()
		content := buf.String()
		buf.Reset()
		bufMu.Unlock()
		if content != "" {
			b.hub.Publish(Event{
				Kind:    KindAssistantMessage,
				AgentID: id,
				Role:    sdk.RoleAssistant,
				Text:    content,
				HTML:    MessageHTML(sdk.RoleAssistant, content),
			})
			// Tell the engine the message is final so extensions can act on it
			// (the `agents` extension renders the finalized markdown bubble in its
			// chat area; `history` records the response). A front end with no tea
			// loop has no other trigger for EventMessageEnd.
			if b.messageEnd != nil {
				b.messageEnd(string(sdk.RoleAssistant), content)
			}
		}
		ev := Event{Kind: KindTurnDone, AgentID: id}
		if err != nil {
			ev.Err = err.Error()
		}
		b.hub.Publish(ev)
	})
}

// onUsage reports a turn beginning or completing. A Started report becomes a
// KindTurnStarted event; a completion is already reported by the agent's
// onDone hook, so it is not duplicated here.
func (b *Bridge) onUsage(u agent.TurnUsage) {
	if !u.Started {
		return
	}
	b.hub.Publish(Event{Kind: KindTurnStarted, AgentID: u.AgentID, Main: u.Main})
}

// HandleContextUsage forwards the pool's context-usage dispatcher to the stream.
// A non-nil notice indicates a context compaction and is surfaced as a
// KindNotify line before the usage snapshot. The caller wires this via
// pool.SetContextUsageDispatcher in serve mode; the Bridge does not claim the
// slot itself because the TUI harness owns it in interactive mode.
func (b *Bridge) HandleContextUsage(cu sdk.ContextUsage, notice *agent.CompactionNotice, _ int) {
	if notice != nil {
		b.hub.Publish(Event{
			Kind:    KindNotify,
			AgentID: b.mainID,
			Text:    compactionNoticeText(notice),
		})
	}
	usage := cu
	b.hub.Publish(Event{Kind: KindContextUsage, AgentID: b.mainID, Usage: &usage})
}

// compactionNoticeText renders a human-readable line for a context compaction,
// matching the wording the TUI harness surfaces so both front ends read alike.
func compactionNoticeText(notice *agent.CompactionNotice) string {
	trigger := notice.Trigger
	if trigger == "" {
		trigger = "unknown"
	}
	return fmt.Sprintf(
		"\U0001F9F9 Context compacted: %d message(s) summarized (trigger: %s)",
		notice.MessagesCompacted,
		trigger,
	)
}
