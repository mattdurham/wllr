package client

// NOTE: Any changes to this file must be reflected in the corresponding SPECS.md or NOTES.md.

import (
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"net/http/httptest"
	"sync"
	"testing"
	"time"

	"github.com/mattdurham/wllr/modules/wire"
)

// fakeInstance is a minimal wllr-shaped server: the REST endpoints the client
// calls plus an SSE /api/stream. Stream behavior is scripted per connection so
// tests can make a connection drop and observe reconnection.
type fakeInstance struct {
	mu sync.Mutex
	// connections counts how many times /api/stream was opened.
	connections int
	// frames[conn] is the scripted event list for that connection index; the
	// handler closes the connection after writing them.
	frames [][]wire.Event
	// seenSince records each `since` query value, to assert resume cursors.
	seenSince []string
	// protocol is the version reported on the ready frame; defaults to
	// wire.ProtocolVersion so a test can present a mismatched instance.
	protocol int

	// REST fixtures.
	agents    []Agent
	models    ModelsResponse
	history   History
	submits   []string
	cancels   []string
	callbacks []string
	lastAuth  string
	tokenOK   string // when set, requests without this bearer get 401

	srv *httptest.Server
}

func newFakeInstance(t *testing.T) *fakeInstance {
	t.Helper()
	f := &fakeInstance{
		protocol: wire.ProtocolVersion,
		models: ModelsResponse{
			Callback: "__wllr:model",
			Current:  map[string]string{"provider": "test", "model": "m1"},
		},
	}
	f.srv = httptest.NewServer(http.HandlerFunc(f.handle))
	t.Cleanup(f.srv.Close)
	return f
}

func (f *fakeInstance) url() string { return f.srv.URL }

func (f *fakeInstance) recordAuth(r *http.Request) {
	f.mu.Lock()
	f.lastAuth = r.Header.Get("Authorization")
	f.mu.Unlock()
}

func (f *fakeInstance) handle(w http.ResponseWriter, r *http.Request) {
	f.recordAuth(r)
	if f.tokenOK != "" && r.Header.Get("Authorization") != "Bearer "+f.tokenOK {
		w.WriteHeader(http.StatusUnauthorized)
		_, _ = w.Write([]byte(`{"error":"unauthorized"}`))
		return
	}
	switch r.URL.Path {
	case "/api/health":
		writeJSON(w, map[string]any{"ok": true, "protocol": wire.ProtocolVersion, "last_seq": 3})
	case "/api/agents":
		writeJSON(w, map[string]any{"agents": f.agents})
	case "/api/models":
		writeJSON(w, f.models)
	case "/api/history":
		writeJSON(w, f.history)
	case "/api/cancel":
		f.mu.Lock()
		f.cancels = append(f.cancels, r.URL.Query().Get("agent"))
		f.mu.Unlock()
		writeJSON(w, map[string]any{"cancelled": true})
	case "/api/submit":
		var body struct{ Content string }
		_ = json.NewDecoder(r.Body).Decode(&body)
		f.mu.Lock()
		f.submits = append(f.submits, body.Content)
		f.mu.Unlock()
		w.WriteHeader(http.StatusAccepted)
		writeJSON(w, map[string]any{"accepted": true})
	case "/api/ui/callback":
		var body struct {
			Callback string `json:"callback"`
		}
		_ = json.NewDecoder(r.Body).Decode(&body)
		f.mu.Lock()
		f.callbacks = append(f.callbacks, body.Callback)
		f.mu.Unlock()
		writeJSON(w, map[string]any{"routed": true})
	case "/api/stream":
		f.handleStream(w, r)
	default:
		w.WriteHeader(http.StatusNotFound)
	}
}

func (f *fakeInstance) handleStream(w http.ResponseWriter, r *http.Request) {
	f.mu.Lock()
	conn := f.connections
	f.connections++
	f.seenSince = append(f.seenSince, r.URL.Query().Get("since"))
	protocol := f.protocol
	var frames []wire.Event
	if conn < len(f.frames) {
		frames = f.frames[conn]
	}
	f.mu.Unlock()

	w.Header().Set("Content-Type", "text/event-stream")
	w.WriteHeader(http.StatusOK)
	fl, _ := w.(http.Flusher)
	// Ready frame, as the real server writes it first.
	writeFrame(w, wire.Event{Kind: wire.KindReady, Protocol: protocol})
	if fl != nil {
		fl.Flush()
	}
	for _, ev := range frames {
		writeFrame(w, ev)
		if fl != nil {
			fl.Flush()
		}
	}
	// Close the connection: the handler returning ends the response body, which
	// the client sees as EOF and reconnects on.
}

func writeFrame(w http.ResponseWriter, ev wire.Event) {
	data, _ := json.Marshal(ev)
	_, _ = fmt.Fprintf(w, "data: %s\n\n", data)
}

func writeJSON(w http.ResponseWriter, v any) {
	w.Header().Set("Content-Type", "application/json")
	_ = json.NewEncoder(w).Encode(v)
}

// newClient builds a client pointed at a fake instance.
func newClient(t *testing.T, id string, f *fakeInstance) *Client {
	t.Helper()
	c, err := New(Config{ID: id, BaseURL: f.url()})
	if err != nil {
		t.Fatalf("New: %v", err)
	}
	return c
}

func TestNewValidatesConfig(t *testing.T) {
	if _, err := New(Config{BaseURL: "http://x"}); err == nil {
		t.Error("expected error for missing ID")
	}
	if _, err := New(Config{ID: "a"}); err == nil {
		t.Error("expected error for missing BaseURL")
	}
	c, err := New(Config{ID: "a", BaseURL: "http://x/"})
	if err != nil {
		t.Fatalf("New: %v", err)
	}
	if c.BaseURL() != "http://x" {
		t.Errorf("trailing slash not trimmed: %q", c.BaseURL())
	}
}

func TestClientHealth(t *testing.T) {
	f := newFakeInstance(t)
	c := newClient(t, "a", f)
	h, err := c.Health(context.Background())
	if err != nil {
		t.Fatalf("Health: %v", err)
	}
	if !h.OK || h.Protocol != wire.ProtocolVersion || h.LastSeq != 3 {
		t.Errorf("unexpected health: %+v", h)
	}
}

func TestClientAgentsModelsHistory(t *testing.T) {
	f := newFakeInstance(t)
	f.agents = []Agent{{ID: "main", Main: true, Running: true, HistoryLen: 4}}
	f.history = History{
		AgentID:  "main",
		Total:    1,
		Messages: []Message{{Role: "assistant", Content: "hi", HTML: "<b>hi</b>"}},
	}
	c := newClient(t, "a", f)

	agents, err := c.Agents(context.Background())
	if err != nil {
		t.Fatalf("Agents: %v", err)
	}
	if len(agents) != 1 || agents[0].ID != "main" || !agents[0].Main {
		t.Errorf("unexpected agents: %+v", agents)
	}

	models, err := c.Models(context.Background())
	if err != nil {
		t.Fatalf("Models: %v", err)
	}
	if models.Callback != "__wllr:model" || models.Current["model"] != "m1" {
		t.Errorf("unexpected models: %+v", models)
	}

	h, err := c.History(context.Background(), HistoryOptions{Agent: "main", Limit: 10})
	if err != nil {
		t.Fatalf("History: %v", err)
	}
	if h.Total != 1 || len(h.Messages) != 1 || h.Messages[0].HTML != "<b>hi</b>" {
		t.Errorf("unexpected history: %+v", h)
	}
}

func TestClientSubmitCancelCallback(t *testing.T) {
	f := newFakeInstance(t)
	c := newClient(t, "a", f)
	ctx := context.Background()

	if err := c.Submit(ctx, "coder", "do work"); err != nil {
		t.Fatalf("Submit: %v", err)
	}
	if err := c.Cancel(ctx, "coder"); err != nil {
		t.Fatalf("Cancel: %v", err)
	}
	if err := c.Callback(ctx, "__wllr:model", "test\x1fm1"); err != nil {
		t.Fatalf("Callback: %v", err)
	}
	f.mu.Lock()
	defer f.mu.Unlock()
	if len(f.submits) != 1 || f.submits[0] != "do work" {
		t.Errorf("submit not recorded: %+v", f.submits)
	}
	if len(f.callbacks) != 1 || f.callbacks[0] != "__wllr:model" {
		t.Errorf("callback not recorded: %+v", f.callbacks)
	}
}

func TestClientSendsBearerToken(t *testing.T) {
	f := newFakeInstance(t)
	f.tokenOK = "secret"
	c, err := New(Config{ID: "a", BaseURL: f.url(), Token: "secret"})
	if err != nil {
		t.Fatalf("New: %v", err)
	}
	if _, err := c.Health(context.Background()); err != nil {
		t.Fatalf("Health with token: %v", err)
	}
	f.mu.Lock()
	got := f.lastAuth
	f.mu.Unlock()
	if got != "Bearer secret" {
		t.Errorf("auth header = %q, want %q", got, "Bearer secret")
	}
}

func TestClientUnauthorizedSurfacesAsHTTPError(t *testing.T) {
	f := newFakeInstance(t)
	f.tokenOK = "secret"
	c, err := New(Config{ID: "a", BaseURL: f.url()}) // no token
	if err != nil {
		t.Fatalf("New: %v", err)
	}
	_, err = c.Health(context.Background())
	if err == nil {
		t.Fatal("expected an error")
	}
	if !IsUnauthorized(err) {
		t.Errorf("IsUnauthorized(%v) = false, want true", err)
	}
}

// TestFollowCollectsUntilCancel follows a scripted instance and checks the ready
// frame plus the scripted events arrive, in order, tagged with the instance.
func TestFollowCollectsUntilCancel(t *testing.T) {
	f := newFakeInstance(t)
	f.frames = [][]wire.Event{{
		{Seq: 1, Kind: wire.KindToken, Text: "a"},
		{Seq: 2, Kind: wire.KindToken, Text: "b"},
	}}
	c := newClient(t, "inst", f)

	ctx, cancel := context.WithCancel(context.Background())
	var got []Tagged
	done := make(chan struct{})
	go func() {
		defer close(done)
		_ = c.Follow(ctx, func(tg Tagged) {
			got = append(got, tg)
			// Stop once we have the two tokens (plus ready).
			if len(got) >= 3 {
				cancel()
			}
		}, FollowOptions{Backoff: Backoff{NoJitter: true}})
	}()
	select {
	case <-done:
	case <-time.After(5 * time.Second):
		t.Fatal("Follow did not return")
	}
	if len(got) < 3 {
		t.Fatalf("got %d events, want >=3: %+v", len(got), got)
	}
	if got[0].Event.Kind != wire.KindReady {
		t.Errorf("first event = %s, want ready", got[0].Event.Kind)
	}
	for _, tg := range got {
		if tg.Instance != "inst" {
			t.Errorf("event not tagged with instance: %+v", tg)
		}
	}
}

// TestFollowReconnectsAndResumesFromCursor proves a dropped connection resumes
// with a `since` cursor equal to the last sequence seen, so no event is
// re-delivered.
func TestFollowReconnectsAndResumesFromCursor(t *testing.T) {
	f := newFakeInstance(t)
	// Connection 0 yields seq 1,2 then drops; connection 1 yields seq 3 then is
	// cancelled by the test.
	f.frames = [][]wire.Event{
		{{Seq: 1, Kind: wire.KindToken, Text: "one"}, {Seq: 2, Kind: wire.KindToken, Text: "two"}},
		{{Seq: 3, Kind: wire.KindToken, Text: "three"}},
	}
	c := newClient(t, "inst", f)

	ctx, cancel := context.WithCancel(context.Background())
	var got []Tagged
	done := make(chan struct{})
	go func() {
		defer close(done)
		_ = c.Follow(ctx, func(tg Tagged) {
			got = append(got, tg)
			if tg.Event.Kind == wire.KindToken && tg.Event.Text == "three" {
				cancel()
			}
		}, FollowOptions{Backoff: Backoff{NoJitter: true, Base: time.Millisecond, Max: 5 * time.Millisecond}})
	}()
	select {
	case <-done:
	case <-time.After(5 * time.Second):
		t.Fatal("Follow did not return")
	}

	f.mu.Lock()
	conns := f.connections
	since := append([]string(nil), f.seenSince...)
	f.mu.Unlock()
	if conns < 2 {
		t.Fatalf("connections = %d, want >=2 (reconnect)", conns)
	}
	// The second connection must resume from seq 2, the last seen.
	if len(since) < 2 || since[1] != "2" {
		t.Errorf("second connection since = %v, want \"2\"", since)
	}
	// No token should appear twice.
	seen := map[string]int{}
	for _, tg := range got {
		if tg.Event.Kind == wire.KindToken {
			seen[tg.Event.Text]++
		}
	}
	for _, txt := range []string{"one", "two", "three"} {
		if seen[txt] != 1 {
			t.Errorf("token %q delivered %d times, want 1 (got=%+v)", txt, seen[txt], got)
		}
	}
}

// TestFollowFlagsGapOnSequenceDiscontinuity proves a skip in sequence numbers is
// surfaced so a consumer can resynchronize.
func TestFollowFlagsGapOnSequenceDiscontinuity(t *testing.T) {
	f := newFakeInstance(t)
	f.frames = [][]wire.Event{{
		{Seq: 5, Kind: wire.KindToken, Text: "after-gap"},
	}}
	// Simulate a resume where the last cursor was 2 but the next event is 5.
	c := newClient(t, "inst", f)

	ctx, cancel := context.WithCancel(context.Background())
	var gapSeen bool
	done := make(chan struct{})
	go func() {
		defer close(done)
		_ = c.Follow(ctx, func(tg Tagged) {
			if tg.Event.Kind == wire.KindToken && tg.Event.Text == "after-gap" {
				gapSeen = tg.Gap
				cancel()
			}
		}, FollowOptions{Since: 2, Backoff: Backoff{NoJitter: true, Base: time.Millisecond}})
	}()
	select {
	case <-done:
	case <-time.After(5 * time.Second):
		t.Fatal("Follow did not return")
	}
	if !gapSeen {
		t.Error("expected Gap=true for a seq discontinuity")
	}
}

func TestMultiplexerRejectsDuplicateAndEmpty(t *testing.T) {
	if _, err := NewMultiplexer(nil, MultiplexerOptions{}); err == nil {
		t.Error("expected error for no instances")
	}
	_, err := NewMultiplexer([]Config{
		{ID: "a", BaseURL: "http://x"},
		{ID: "a", BaseURL: "http://y"},
	}, MultiplexerOptions{})
	if err == nil {
		t.Error("expected error for duplicate instance ID")
	}
}

// TestMultiplexerMergesInstances is the core acceptance check: two instances'
// streams merge into one channel with unambiguous, instance-tagged identities.
func TestMultiplexerMergesInstances(t *testing.T) {
	fa := newFakeInstance(t)
	fa.frames = [][]wire.Event{{{Seq: 1, Kind: wire.KindToken, AgentID: "main", Text: "from-A"}}}
	fb := newFakeInstance(t)
	fb.frames = [][]wire.Event{{{Seq: 1, Kind: wire.KindToken, AgentID: "main", Text: "from-B"}}}

	mux, err := NewMultiplexer([]Config{
		{ID: "alpha", BaseURL: fa.url()},
		{ID: "beta", BaseURL: fb.url()},
	}, MultiplexerOptions{Follow: FollowOptions{Backoff: Backoff{NoJitter: true, Base: time.Millisecond}}})
	if err != nil {
		t.Fatalf("NewMultiplexer: %v", err)
	}

	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	go func() { _ = mux.Run(ctx) }()

	byText := map[string]string{} // text -> agent key
	need := map[string]bool{"from-A": false, "from-B": false}
	timeout := time.After(5 * time.Second)
	for len(byText) < 2 {
		select {
		case tg := <-mux.Events():
			if tg.Event.Kind == wire.KindToken {
				byText[tg.Event.Text] = tg.AgentKey()
			}
		case <-timeout:
			t.Fatalf("timed out; got %+v", byText)
		}
	}
	if byText["from-A"] != "alpha/main" {
		t.Errorf("from-A key = %q, want alpha/main", byText["from-A"])
	}
	if byText["from-B"] != "beta/main" {
		t.Errorf("from-B key = %q, want beta/main", byText["from-B"])
	}
	_ = need
}

func TestMultiplexerEventsClosedAfterRun(t *testing.T) {
	f := newFakeInstance(t)
	f.frames = [][]wire.Event{nil}
	mux, err := NewMultiplexer([]Config{{ID: "x", BaseURL: f.url()}}, MultiplexerOptions{
		Follow: FollowOptions{Backoff: Backoff{NoJitter: true, Base: time.Millisecond}},
	})
	if err != nil {
		t.Fatalf("NewMultiplexer: %v", err)
	}
	ctx, cancel := context.WithCancel(context.Background())
	runDone := make(chan error, 1)
	go func() { runDone <- mux.Run(ctx) }()
	// Cancel quickly; the merged channel must close so a range loop ends.
	cancel()
	select {
	case <-runDone:
	case <-time.After(5 * time.Second):
		t.Fatal("Run did not return after cancel")
	}
	select {
	case _, ok := <-mux.Events():
		for ok {
			_, ok = <-mux.Events()
		}
	case <-time.After(5 * time.Second):
		t.Fatal("Events channel did not close")
	}
}

// TestMultiplexerReturnsOnFollowerRejection proves a single follower's OnConnect
// rejection terminates the whole multiplexer: Run returns that error without any
// external cancel, because the rejection cancels the shared run context and the
// other follower unwinds.
func TestMultiplexerReturnsOnFollowerRejection(t *testing.T) {
	fa := newFakeInstance(t) // reports the expected protocol: accepted
	fb := newFakeInstance(t)
	fb.protocol = wire.ProtocolVersion + 1 // rejected by the version gate

	mux, err := NewMultiplexer([]Config{
		{ID: "alpha", BaseURL: fa.url()},
		{ID: "beta", BaseURL: fb.url()},
	}, MultiplexerOptions{Follow: FollowOptions{
		OnConnect: func(protocol int) error {
			if protocol != wire.ProtocolVersion {
				return fmt.Errorf("client: instance protocol %d unsupported", protocol)
			}
			return nil
		},
		Backoff: Backoff{NoJitter: true, Base: time.Millisecond, Max: 5 * time.Millisecond},
	}})
	if err != nil {
		t.Fatalf("NewMultiplexer: %v", err)
	}

	// No external cancel: Run must return on the rejection alone. Before the fix
	// the accepted follower kept reconnecting and Run blocked on wg.Wait.
	runDone := make(chan error, 1)
	go func() { runDone <- mux.Run(context.Background()) }()

	select {
	case err := <-runDone:
		if err == nil {
			t.Fatal("Run returned nil, want the follower's rejection error")
		}
	case <-time.After(5 * time.Second):
		t.Fatal("Run did not return on a follower rejection (blocked on wg.Wait)")
	}
}

func TestMultiplexerStopUnblocksSaturatedConsumer(t *testing.T) {
	f := newFakeInstance(t)
	// More events than the merged buffer holds, with a consumer that never
	// reads, so a follower blocks in emit on a saturated channel.
	frames := make([]wire.Event, 0, 64)
	for i := 0; i < 64; i++ {
		frames = append(frames, wire.Event{Seq: int64(i + 1), Kind: wire.KindToken, AgentID: "main", Text: "x"})
	}
	f.frames = [][]wire.Event{frames}

	mux, err := NewMultiplexer([]Config{{ID: "x", BaseURL: f.url()}}, MultiplexerOptions{
		Buffer: 1,
		Follow: FollowOptions{Backoff: Backoff{NoJitter: true, Base: time.Millisecond}},
	})
	if err != nil {
		t.Fatalf("NewMultiplexer: %v", err)
	}

	runDone := make(chan error, 1)
	go func() { runDone <- mux.Run(context.Background()) }()

	// Wait until the merged buffer is full and the follower is blocked on the
	// next emit (nothing is reading).
	deadline := time.After(5 * time.Second)
	for len(mux.Events()) == 0 {
		select {
		case <-deadline:
			t.Fatal("no event reached the merged buffer")
		default:
			time.Sleep(time.Millisecond)
		}
	}
	time.Sleep(20 * time.Millisecond)

	mux.Stop()
	select {
	case err := <-runDone:
		if err != nil {
			t.Fatalf("Run returned %v, want nil after Stop", err)
		}
	case <-time.After(5 * time.Second):
		t.Fatal("Run did not return after Stop with a saturated consumer")
	}
}

func TestMultiplexerInstancesSorted(t *testing.T) {
	f := newFakeInstance(t)
	mux, err := NewMultiplexer([]Config{
		{ID: "zeta", BaseURL: f.url()},
		{ID: "alpha", BaseURL: f.url()},
	}, MultiplexerOptions{})
	if err != nil {
		t.Fatalf("NewMultiplexer: %v", err)
	}
	got := mux.Instances()
	if len(got) != 2 || got[0] != "alpha" || got[1] != "zeta" {
		t.Errorf("Instances = %v, want [alpha zeta]", got)
	}
}

func TestAgentKeyAndParse(t *testing.T) {
	tg := Tagged{Instance: "ci", Event: wire.Event{AgentID: "main"}}
	if tg.AgentKey() != "ci/main" {
		t.Errorf("AgentKey = %q", tg.AgentKey())
	}
	if r := ParseAgentRef("ci/main"); r.Instance != "ci" || r.Agent != "main" {
		t.Errorf("ParseAgentRef = %+v", r)
	}
	if r := ParseAgentRef("lonely"); r.Instance != "lonely" || r.Agent != "" {
		t.Errorf("ParseAgentRef(no sep) = %+v", r)
	}
	if s := (AgentRef{Instance: "a"}).String(); s != "a" {
		t.Errorf("String = %q", s)
	}
}
