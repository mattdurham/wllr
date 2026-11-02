package agent

// NOTE: Any changes to this file must be reflected in the corresponding SPECS.md or NOTES.md.

import (
	"context"
	"strings"
	"testing"
	"time"

	"charm.land/fantasy"
)

const repeatedOutputParagraph = "The compaction gate is still waiting with 16 active workers and 40 pending jobs. " +
	"I’ll inspect the queue’s in-flight bookkeeping and scanner’s repeated offers to determine whether the barrier is waiting on real work or a queue bookkeeping bug."

type repeatingOutputLM struct {
	emitted int
}

func (lm *repeatingOutputLM) Generate(context.Context, fantasy.Call) (*fantasy.Response, error) {
	return &fantasy.Response{}, nil
}

func (lm *repeatingOutputLM) GenerateObject(context.Context, fantasy.ObjectCall) (*fantasy.ObjectResponse, error) {
	return &fantasy.ObjectResponse{}, nil
}

func (lm *repeatingOutputLM) StreamObject(context.Context, fantasy.ObjectCall) (fantasy.ObjectStreamResponse, error) {
	return func(func(fantasy.ObjectStreamPart) bool) {}, nil
}

func (lm *repeatingOutputLM) Provider() string { return "fake" }
func (lm *repeatingOutputLM) Model() string    { return "repeating-output" }

func (lm *repeatingOutputLM) Stream(ctx context.Context, _ fantasy.Call) (fantasy.StreamResponse, error) {
	return func(yield func(fantasy.StreamPart) bool) {
		if !yield(fantasy.StreamPart{Type: fantasy.StreamPartTypeTextStart, ID: "0"}) {
			return
		}
		for range 12 {
			select {
			case <-ctx.Done():
				return
			default:
			}
			lm.emitted++
			if !yield(fantasy.StreamPart{
				Type:  fantasy.StreamPartTypeTextDelta,
				ID:    "0",
				Delta: repeatedOutputParagraph + "\n\n",
			}) {
				return
			}
		}
		yield(fantasy.StreamPart{Type: fantasy.StreamPartTypeTextEnd, ID: "0"})
		yield(fantasy.StreamPart{Type: fantasy.StreamPartTypeFinish, FinishReason: fantasy.FinishReasonStop})
	}, nil
}

func TestOutputLoopDetector_ThirdRepeatedParagraph(t *testing.T) {
	d := newOutputLoopDetector()
	paragraph := "Warmup is still waiting for the 1 MB minute to close. " +
		"I’ll let it complete, then capture profiles during actual 1 GB compaction work."
	for repeat := 0; repeat < 2; repeat++ {
		if d.Observe(paragraph + "\n\n") {
			t.Fatalf("detector triggered on repetition %d, before the third paragraph", repeat+1)
		}
	}
	if !d.Observe(paragraph + "\n\n") {
		t.Fatal("detector did not trigger on the third identical paragraph")
	}
}

func TestOutputLoopDetector_SplitDeltas(t *testing.T) {
	d := newOutputLoopDetector()
	stream := strings.Repeat(repeatedOutputParagraph+"\n\n", 3)
	triggered := false
	for len(stream) > 0 {
		n := 17
		if n > len(stream) {
			n = len(stream)
		}
		if d.Observe(stream[:n]) {
			triggered = true
			break
		}
		stream = stream[n:]
	}
	if !triggered {
		t.Fatal("detector missed repeated paragraphs split across deltas")
	}
	if stream == "" {
		t.Fatal("detector did not stop before consuming the entire repeated stream")
	}
}

func TestOutputLoopDetector_DifferentParagraphsDoNotTrigger(t *testing.T) {
	d := newOutputLoopDetector()
	for _, paragraph := range []string{
		"The first inspection found a saturated queue with sixteen active workers and useful compaction progress.",
		"The second inspection captured a CPU profile and identified the storage scan as the next investigation target.",
		"The third inspection changed the experiment and recorded a distinct result instead of repeating prior work.",
	} {
		if d.Observe(paragraph + "\n\n") {
			t.Fatalf("detector triggered on distinct paragraph %q", paragraph)
		}
	}
}

func TestAgent_RepeatedAssistantOutputStopsRun(t *testing.T) {
	lm := &repeatingOutputLM{}
	pool := NewPool()
	a, err := pool.Spawn("main", lm, SpawnOpts{ModelName: "fake-model", ContextWindow: 100_000})
	if err != nil {
		t.Fatalf("Spawn: %v", err)
	}
	done := make(chan error, 1)
	a.SetOnDone(func(err error) { done <- err })
	a.Submit(context.Background(), "investigate compaction")

	select {
	case err := <-done:
		if err == nil || !strings.Contains(strings.ToLower(err.Error()), "loop") {
			t.Fatalf("repeated assistant output err = %v, want an output-loop termination", err)
		}
	case <-time.After(2 * time.Second):
		t.Fatal("agent did not stop after repeated assistant output")
	}
	if lm.emitted > 3 {
		t.Fatalf("stream emitted %d identical paragraphs before stopping, want at most 3", lm.emitted)
	}
	if a.IsRunning() {
		t.Fatal("agent remained running after output-loop termination")
	}
}
