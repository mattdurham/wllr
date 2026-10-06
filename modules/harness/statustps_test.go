package harness

// NOTE: Any changes to this file must be reflected in the corresponding SPECS.md or NOTES.md.

import (
	"context"
	"strings"
	"testing"
	"time"

	"charm.land/fantasy"

	"github.com/mattdurham/wllr/modules/agent"
)

func TestFormatTps(t *testing.T) {
	cases := []struct {
		name string
		in   float64
		want string
	}{
		{"zero hides", 0, ""},
		{"negative hides", -2, ""},
		{"sub-one shows floor", 0.4, "<1 t/s"},
		{"rounds to integer", 87.4, "87 t/s"},
		{"large rate", 1234.5, "1234 t/s"},
	}
	for _, tc := range cases {
		if got := formatTps(tc.in); got != tc.want {
			t.Errorf("%s: formatTps(%v) = %q, want %q", tc.name, tc.in, got, tc.want)
		}
	}
}

// tpsHarnessLM streams single-char tokens with a real sleep between them and
// reports output usage on the finish part — slow enough for the tracker to
// have a measurable rate mid-turn.
type tpsHarnessLM struct {
	output int64
}

func (t *tpsHarnessLM) Model() string    { return "tps-harness" }
func (t *tpsHarnessLM) Provider() string { return "test" }

func (t *tpsHarnessLM) Stream(_ context.Context, _ fantasy.Call) (fantasy.StreamResponse, error) {
	return func(yield func(fantasy.StreamPart) bool) {
		for i := 0; i < 10; i++ {
			if !yield(fantasy.StreamPart{Type: fantasy.StreamPartTypeTextDelta, Delta: "x"}) {
				return
			}
			time.Sleep(60 * time.Millisecond)
		}
		yield(fantasy.StreamPart{
			Type:         fantasy.StreamPartTypeFinish,
			FinishReason: fantasy.FinishReasonStop,
			Usage: fantasy.Usage{
				InputTokens:  50,
				OutputTokens: t.output,
				TotalTokens:  50 + t.output,
			},
		})
	}, nil
}

func (t *tpsHarnessLM) Generate(_ context.Context, _ fantasy.Call) (*fantasy.Response, error) {
	return &fantasy.Response{}, nil
}

func (t *tpsHarnessLM) GenerateObject(_ context.Context, _ fantasy.ObjectCall) (*fantasy.ObjectResponse, error) {
	return nil, nil
}

func (t *tpsHarnessLM) StreamObject(_ context.Context, _ fantasy.ObjectCall) (fantasy.ObjectStreamResponse, error) {
	return nil, nil
}

// TestModel_Update_StreamTickMsg_PropagatesLiveTps drives a real mid-turn
// stream and verifies the tick handler copies the agent's live rate into the
// "tps" status key.
func TestModel_Update_StreamTickMsg_PropagatesLiveTps(t *testing.T) {
	pool := agent.NewPool()
	main, err := pool.Spawn("main", &tpsHarnessLM{output: 100}, agent.SpawnOpts{})
	if err != nil {
		t.Fatalf("Spawn: %v", err)
	}
	m := New(pool, "main", nil)

	done := make(chan error, 1)
	main.SetOnDone(func(e error) { done <- e })
	if err := pool.Send("main", "hi"); err != nil {
		t.Fatalf("Send: %v", err)
	}

	// Poll the agent's tracker (not a fixed sleep) so the assertion below is
	// deterministic: the moment the agent reports a live rate, the tick must
	// be able to propagate it.
	deadline := time.Now().Add(5 * time.Second)
	for main.StreamTps() <= 0 {
		if time.Now().After(deadline) {
			t.Fatal("agent never reported a live tps")
		}
		time.Sleep(10 * time.Millisecond)
	}

	m.streaming = true // the UI streaming flag gates tick re-arming
	m, _ = callUpdate(m, streamTickMsg{})
	if got := m.live.getStatus("tps"); got == "" {
		t.Fatal("streamTickMsg did not propagate the live tps into the status map")
	}

	main.Cancel()
	select {
	case <-done:
	case <-time.After(5 * time.Second):
		t.Fatal("timeout waiting for cancelled turn")
	}
}

// TestModel_Update_StreamDoneMsg_FreezesTpsFromPool verifies the done handler
// freezes the display at the agent's exact end-of-turn rate.
func TestModel_Update_StreamDoneMsg_FreezesTpsFromPool(t *testing.T) {
	pool := agent.NewPool()
	main, err := pool.Spawn("main", &tpsHarnessLM{output: 100}, agent.SpawnOpts{})
	if err != nil {
		t.Fatalf("Spawn: %v", err)
	}
	done := make(chan error, 1)
	main.SetOnDone(func(e error) { done <- e })
	if err := pool.Send("main", "hi"); err != nil {
		t.Fatalf("Send: %v", err)
	}
	select {
	case <-done:
	case <-time.After(10 * time.Second):
		t.Fatal("timeout waiting for turn")
	}

	m := New(pool, "main", nil)
	m, _ = callUpdate(m, StreamDoneMsg{Err: nil})
	got := m.live.getStatus("tps")
	if got == "" || !strings.HasSuffix(got, " t/s") {
		t.Fatalf("tps status after done = %q, want a frozen '<n> t/s' value", got)
	}
}

// TestModel_Update_TpsHiddenWithoutMeasuredStream verifies both handlers
// leave the status empty — the wasm hides the segment — when nothing was
// measured (no turn, and the bundled mock's instant no-usage stream).
func TestModel_Update_TpsHiddenWithoutMeasuredStream(t *testing.T) {
	m := newTestModel()

	m, _ = callUpdate(m, streamTickMsg{})
	if v := m.live.getStatus("tps"); v != "" {
		t.Errorf("tps status before any turn = %q, want empty", v)
	}

	m, _ = callUpdate(m, StreamDoneMsg{Err: nil})
	if v := m.live.getStatus("tps"); v != "" {
		t.Errorf("tps status after instant no-usage stream = %q, want empty", v)
	}
}
