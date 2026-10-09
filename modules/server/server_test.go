package server

// NOTE: Any changes to this file must be reflected in the corresponding SPECS.md or NOTES.md.

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"github.com/mattdurham/wllr/modules/agent"
	"github.com/mattdurham/wllr/modules/testutil"
)

// contextWithCancel returns a cancellable context, keeping the test body free
// of a direct context import at each call site.
func contextWithCancel() (context.Context, context.CancelFunc) {
	return context.WithCancel(context.Background())
}

// newTestPool returns a pool with a single root agent backed by a FakeLM that
// answers every turn with "hello world" (streamed word by word).
func newTestPool(t *testing.T) (*agent.AgentPool, *testutil.FakeLM) {
	t.Helper()
	lm := testutil.NewFakeLMWithResponses("hello world")
	pool := agent.NewPool()
	pool.SetDefaultModelName("fake-model")
	pool.SetModelContextWindow("fake-model", 100000)
	if _, err := pool.Spawn(agent.MainAgentID, lm, agent.SpawnOpts{
		ModelName:     "fake-model",
		ContextWindow: 100000,
		TurnTimeout:   -1,
	}); err != nil {
		t.Fatalf("spawn main agent: %v", err)
	}
	return pool, lm
}

// collectUntilTurnDone drains ch until a KindTurnDone arrives or the deadline
// passes, returning every event seen.
func collectUntilTurnDone(t *testing.T, ch <-chan Event) []Event {
	t.Helper()
	var events []Event
	deadline := time.After(10 * time.Second)
	for {
		select {
		case ev := <-ch:
			events = append(events, ev)
			if ev.Kind == KindTurnDone {
				return events
			}
		case <-deadline:
			t.Fatalf("timed out waiting for turn_done; got %d events", len(events))
			return events
		}
	}
}

func TestServerStreamsTokensAndTurnDone(t *testing.T) {
	pool, _ := newTestPool(t)
	srv := New(pool, Options{})
	srv.Start()

	ch, _, cancel := srv.Hub().Subscribe(0)
	defer cancel()

	if err := pool.Send(agent.MainAgentID, "hi"); err != nil {
		t.Fatalf("send: %v", err)
	}
	events := collectUntilTurnDone(t, ch)

	var tokens strings.Builder
	var sawUserMessage, sawTurnStarted bool
	lastSeq := int64(0)
	for _, ev := range events {
		if ev.Seq <= lastSeq {
			t.Fatalf("sequence not increasing: %d after %d", ev.Seq, lastSeq)
		}
		lastSeq = ev.Seq
		switch ev.Kind {
		case KindUserMessage:
			sawUserMessage = true
			if ev.AgentID != agent.MainAgentID {
				t.Fatalf("user_message agent = %q, want %q", ev.AgentID, agent.MainAgentID)
			}
		case KindTurnStarted:
			sawTurnStarted = true
		case KindToken:
			tokens.WriteString(ev.Text)
		}
	}
	if !sawUserMessage {
		t.Errorf("no user_message event")
	}
	if !sawTurnStarted {
		t.Errorf("no turn_started event")
	}
	if got := tokens.String(); got != "hello world" {
		t.Errorf("streamed tokens = %q, want %q", got, "hello world")
	}
}

// TestServerStreamsRenderedAssistantMessage proves a completed turn publishes a
// rendered assistant message *before* turn_done, carrying both the raw markdown
// (Text) and its HTML form (HTML). The ordering matters: the client replaces the
// in-flight bubble on this frame, then clears its streaming state on turn_done.
func TestServerStreamsRenderedAssistantMessage(t *testing.T) {
	pool, _ := newTestPool(t)
	srv := New(pool, Options{})
	srv.Start()

	ch, _, cancel := srv.Hub().Subscribe(0)
	defer cancel()

	if err := pool.Send(agent.MainAgentID, "hi"); err != nil {
		t.Fatalf("send: %v", err)
	}
	events := collectUntilTurnDone(t, ch)

	msgIdx, doneIdx := -1, -1
	for i, ev := range events {
		switch ev.Kind {
		case KindAssistantMessage:
			msgIdx = i
		case KindTurnDone:
			doneIdx = i
		}
	}
	if msgIdx < 0 {
		t.Fatalf("no assistant_message frame among %d events", len(events))
	}
	if msgIdx > doneIdx {
		t.Errorf("assistant_message (index %d) must precede turn_done (index %d)", msgIdx, doneIdx)
	}
	msg := events[msgIdx]
	if msg.Text != "hello world" {
		t.Errorf("raw text = %q, want %q", msg.Text, "hello world")
	}
	if !strings.Contains(msg.HTML, "hello world") {
		t.Errorf("rendered HTML missing text: %q", msg.HTML)
	}
	if strings.Contains(msg.HTML, "\x1b") {
		t.Errorf("rendered HTML carries raw escapes: %q", msg.HTML)
	}
	if msg.AgentID != agent.MainAgentID {
		t.Errorf("agent = %q, want %q", msg.AgentID, agent.MainAgentID)
	}
}

func TestServerHistoryReflectsCompletedTurn(t *testing.T) {
	pool, _ := newTestPool(t)
	srv := New(pool, Options{})
	srv.Start()

	ch, _, cancel := srv.Hub().Subscribe(0)
	defer cancel()
	if err := pool.Send(agent.MainAgentID, "hi"); err != nil {
		t.Fatalf("send: %v", err)
	}
	collectUntilTurnDone(t, ch)

	rr := httptest.NewRecorder()
	srv.Handler().ServeHTTP(rr, httptest.NewRequest(http.MethodGet, "/api/history", nil))
	if rr.Code != http.StatusOK {
		t.Fatalf("history status = %d, want 200", rr.Code)
	}
	var body struct {
		AgentID  string        `json:"agent_id"`
		Total    int           `json:"total"`
		Messages []interface{} `json:"messages"`
	}
	if err := json.Unmarshal(rr.Body.Bytes(), &body); err != nil {
		t.Fatalf("decode history: %v", err)
	}
	if body.AgentID != agent.MainAgentID {
		t.Errorf("agent_id = %q, want %q", body.AgentID, agent.MainAgentID)
	}
	if body.Total < 2 {
		t.Errorf("total = %d, want >= 2 (user + assistant)", body.Total)
	}
}

func TestServerAgentsListsRoot(t *testing.T) {
	pool, _ := newTestPool(t)
	srv := New(pool, Options{})

	rr := httptest.NewRecorder()
	srv.Handler().ServeHTTP(rr, httptest.NewRequest(http.MethodGet, "/api/agents", nil))
	if rr.Code != http.StatusOK {
		t.Fatalf("agents status = %d, want 200", rr.Code)
	}
	var body struct {
		Agents []agentView `json:"agents"`
	}
	if err := json.Unmarshal(rr.Body.Bytes(), &body); err != nil {
		t.Fatalf("decode agents: %v", err)
	}
	if len(body.Agents) != 1 || body.Agents[0].ID != agent.MainAgentID {
		t.Fatalf("agents = %+v, want single main", body.Agents)
	}
	if !body.Agents[0].Main {
		t.Errorf("main flag not set on root agent")
	}
}

func TestServerSubmitRejectsEmptyContent(t *testing.T) {
	pool, _ := newTestPool(t)
	srv := New(pool, Options{})

	req := httptest.NewRequest(http.MethodPost, "/api/submit", strings.NewReader(`{"content":"  "}`))
	rr := httptest.NewRecorder()
	srv.Handler().ServeHTTP(rr, req)
	if rr.Code != http.StatusBadRequest {
		t.Fatalf("status = %d, want 400", rr.Code)
	}
}

func TestServerSubmitRejectsUnknownAgent(t *testing.T) {
	pool, _ := newTestPool(t)
	srv := New(pool, Options{})

	req := httptest.NewRequest(http.MethodPost, "/api/submit", strings.NewReader(`{"agent":"nope","content":"hi"}`))
	rr := httptest.NewRecorder()
	srv.Handler().ServeHTTP(rr, req)
	if rr.Code != http.StatusNotFound {
		t.Fatalf("status = %d, want 404", rr.Code)
	}
}

func TestServerSubmitStartsTurn(t *testing.T) {
	pool, _ := newTestPool(t)
	srv := New(pool, Options{})
	srv.Start()

	ch, _, cancel := srv.Hub().Subscribe(0)
	defer cancel()

	req := httptest.NewRequest(http.MethodPost, "/api/submit", strings.NewReader(`{"content":"hi"}`))
	rr := httptest.NewRecorder()
	srv.Handler().ServeHTTP(rr, req)
	if rr.Code != http.StatusAccepted {
		t.Fatalf("status = %d, want 202", rr.Code)
	}
	events := collectUntilTurnDone(t, ch)
	if len(events) == 0 {
		t.Fatalf("no events after submit")
	}
}

func TestServerCancelUnknownAgent(t *testing.T) {
	pool, _ := newTestPool(t)
	srv := New(pool, Options{})

	req := httptest.NewRequest(http.MethodPost, "/api/cancel", strings.NewReader(`{"agent":"ghost"}`))
	rr := httptest.NewRecorder()
	srv.Handler().ServeHTTP(rr, req)
	if rr.Code != http.StatusNotFound {
		t.Fatalf("status = %d, want 404", rr.Code)
	}
}

func TestServerAuthRequired(t *testing.T) {
	pool, _ := newTestPool(t)
	srv := New(pool, Options{Token: "secret"})

	// No token: rejected.
	rr := httptest.NewRecorder()
	srv.Handler().ServeHTTP(rr, httptest.NewRequest(http.MethodGet, "/api/health", nil))
	if rr.Code != http.StatusUnauthorized {
		t.Fatalf("no-token status = %d, want 401", rr.Code)
	}

	// Bearer header: allowed.
	req := httptest.NewRequest(http.MethodGet, "/api/health", nil)
	req.Header.Set("Authorization", "Bearer secret")
	rr = httptest.NewRecorder()
	srv.Handler().ServeHTTP(rr, req)
	if rr.Code != http.StatusOK {
		t.Fatalf("bearer status = %d, want 200", rr.Code)
	}

	// Query param: allowed.
	rr = httptest.NewRecorder()
	srv.Handler().ServeHTTP(rr, httptest.NewRequest(http.MethodGet, "/api/health?token=secret", nil))
	if rr.Code != http.StatusOK {
		t.Fatalf("query-token status = %d, want 200", rr.Code)
	}
}

func TestServerStreamReplaysBacklogFromSince(t *testing.T) {
	pool, _ := newTestPool(t)
	srv := New(pool, Options{})
	srv.Start()

	ch, _, cancel := srv.Hub().Subscribe(0)
	defer cancel()
	if err := pool.Send(agent.MainAgentID, "hi"); err != nil {
		t.Fatalf("send: %v", err)
	}
	events := collectUntilTurnDone(t, ch)
	if len(events) < 2 {
		t.Fatalf("need at least 2 events to test replay, got %d", len(events))
	}

	// Reconnect with a cursor just before the last event: the SSE handler must
	// replay exactly the events after that sequence.
	since := events[len(events)-2].Seq
	req := httptest.NewRequest(http.MethodGet, "/api/stream?since="+itoa(since), nil)
	ctx, cancelReq := contextWithCancel()
	defer cancelReq()
	rr := httptest.NewRecorder()
	// Cancel the request context so the handler returns after replaying.
	go func() {
		time.Sleep(200 * time.Millisecond)
		cancelReq()
	}()
	srv.Handler().ServeHTTP(rr, req.WithContext(ctx))

	body := rr.Body.String()
	if !strings.Contains(body, "ready") {
		t.Errorf("stream missing ready frame: %q", body)
	}
	if !strings.Contains(body, "turn_done") {
		t.Errorf("stream missing replayed turn_done frame: %q", body)
	}
}

// TestServerInboxHandlersRequirePool proves the three inbox handlers answer the
// documented 503 (rather than panicking on a nil *AgentPool) when the server was
// built without a pool, matching handleHistory/handleSubmit/handleCancel.
func TestServerInboxHandlersRequirePool(t *testing.T) {
	srv := New(nil, Options{})
	cases := []struct {
		method string
		path   string
	}{
		{http.MethodGet, "/api/inbox"},
		{http.MethodPost, "/api/inbox/clear"},
		{http.MethodPost, "/api/inbox/edit"},
	}
	for _, tc := range cases {
		rec := httptest.NewRecorder()
		srv.Handler().ServeHTTP(rec, httptest.NewRequest(tc.method, tc.path, strings.NewReader(`{}`)))
		if rec.Code != http.StatusServiceUnavailable {
			t.Errorf("%s %s = %d, want %d", tc.method, tc.path, rec.Code, http.StatusServiceUnavailable)
		}
	}
}

// itoa is a tiny local int-to-string helper to avoid importing strconv in tests.
func itoa(n int64) string {
	if n == 0 {
		return "0"
	}
	neg := n < 0
	if neg {
		n = -n
	}
	var buf [20]byte
	i := len(buf)
	for n > 0 {
		i--
		buf[i] = byte('0' + n%10)
		n /= 10
	}
	if neg {
		i--
		buf[i] = '-'
	}
	return string(buf[i:])
}
