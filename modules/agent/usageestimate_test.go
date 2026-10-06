package agent

// NOTE: Any changes to this file must be reflected in the corresponding SPECS.md or NOTES.md.

import (
	"context"
	"fmt"
	"sync"
	"sync/atomic"
	"testing"
	"time"

	"charm.land/fantasy"

	"github.com/mattdurham/wllr/modules/sdk"
	"github.com/mattdurham/wllr/modules/testutil"
)

// blockingLM wraps FakeLM and blocks inside Stream until released, so a test
// can observe agent state during the provider call — the window between turn
// start and the first OnStepFinish, where the baseline seed is the only
// context-usage value that exists. Release channels are indexed by call
// number (release(n) unblocks call n) so turns are independent and there is
// no queue race between Stream's wait and the test's release.
type blockingLM struct {
	*testutil.FakeLM
	started      chan struct{}
	releaseChans []chan struct{}
	callN        atomic.Int64
}

var _ fantasy.LanguageModel = (*blockingLM)(nil)

func newBlockingLM(lm *testutil.FakeLM, turns int) *blockingLM {
	b := &blockingLM{FakeLM: lm, started: make(chan struct{}, 8)}
	for range turns {
		b.releaseChans = append(b.releaseChans, make(chan struct{}))
	}
	return b
}

func (b *blockingLM) Stream(ctx context.Context, call fantasy.Call) (fantasy.StreamResponse, error) {
	n := b.callN.Add(1)
	select {
	case b.started <- struct{}{}:
	default:
	}
	<-b.releaseChans[n-1]
	return b.FakeLM.Stream(ctx, call)
}

// release unblocks the n-th Stream call (1-based, in call order).
func (b *blockingLM) release(n int) { close(b.releaseChans[n-1]) }

// TestSeedUsage_BaselineVisibleBeforeFirstStep pins the fresh-session ctx
// fix: before the first provider step reports, the context indicator must
// already show the baseline — system prompt + tool definitions + the
// conversation are on the wire, so 0 was a lie. The seed is observable inside
// Stream (blockingLM), the only window where no reported usage exists yet.
// A second turn must NOT re-seed: once real usage is reported it is
// authoritative, and re-seeding each turn would bounce the display up to the
// chars/4 overestimate at every turn start.
func TestSeedUsage_BaselineVisibleBeforeFirstStep(t *testing.T) {
	lm := testutil.NewFakeLMWithResponses("all done")
	blm := newBlockingLM(lm, 2)
	pool := NewPool()

	var mu sync.Mutex
	var dispatches []sdk.ContextUsage
	pool.SetContextUsageDispatcher(func(cu sdk.ContextUsage, _ *CompactionNotice, _ int) {
		mu.Lock()
		dispatches = append(dispatches, cu)
		mu.Unlock()
	})

	a, err := pool.Spawn(MainAgentID, blm, SpawnOpts{ModelName: "fake-model", ContextWindow: 100_000})
	if err != nil {
		t.Fatalf("Spawn: %v", err)
	}
	const sys = "You are a helpful assistant with a deliberately long system prompt for seed math."
	content1 := fmt.Sprintf("turn one prompt padded to a measurable length %040d", 1)
	expectedSeed := int64((len(sys) + len(content1)) / 4)
	a.SetSystemPrompt(sys)

	done1 := make(chan error, 1)
	a.SetOnDone(func(err error) { done1 <- err })
	a.Submit(context.Background(), content1)

	// Inside Stream, before any step reported: the seed must be installed.
	<-blm.started
	waitForLiveCtx(t, func() bool { return a.LastUsage().InputTokens == expectedSeed }, "baseline seed")
	mu.Lock()
	seedDispatches := len(dispatches)
	var seedWindowOK bool
	for _, cu := range dispatches {
		if cu.ContextWindow == 100_000 && cu.InputTokens == expectedSeed {
			seedWindowOK = true
		}
	}
	mu.Unlock()
	if seedDispatches == 0 || !seedWindowOK {
		t.Fatalf("seed dispatches = %d (windowOK=%v), want >=1 carrying window 100000 and input %d",
			seedDispatches, seedWindowOK, expectedSeed)
	}

	blm.release(1)
	select {
	case err := <-done1:
		if err != nil {
			t.Fatalf("turn 1 errored: %v", err)
		}
	case <-time.After(10 * time.Second):
		t.Fatal("turn 1 did not finish")
	}
	real1 := a.LastUsage().InputTokens
	if real1 == 0 {
		t.Fatalf("turn 1 stored no real usage: %+v", a.LastUsage())
	}

	// Turn 2 with a different-length prompt: during its provider call the
	// stored value must still be turn 1's real usage — no re-seed.
	content2 := fmt.Sprintf("turn two prompt is considerably longer so a re-seed would be visible %080d", 2)
	done2 := make(chan error, 1)
	a.SetOnDone(func(err error) { done2 <- err })
	a.Submit(context.Background(), content2)
	<-blm.started
	if got := a.LastUsage().InputTokens; got != real1 {
		t.Fatalf("turn 2 mid-provider LastUsage input = %d, want turn 1 real %d (re-seed must be disabled)", got, real1)
	}
	blm.release(2)
	select {
	case err := <-done2:
		if err != nil {
			t.Fatalf("turn 2 errored: %v", err)
		}
	case <-time.After(10 * time.Second):
		t.Fatal("turn 2 did not finish")
	}
	// Turn 2's real input (derived from content2's length) replaced turn 1's.
	if want := int64(len(content2) / 4); a.LastUsage().InputTokens < want/2 || a.LastUsage().InputTokens == 0 {
		t.Fatalf("turn 2 final usage = %d, want turn 2 real (>= %d)", a.LastUsage().InputTokens, want/2)
	}
}

// TestSeedUsage_ReplacementNotPeak pins the flag semantics: the first real
// report replaces the seed outright (the seed deliberately overestimates, so
// a peak rule would pin the display high), later steps peak as usual, and
// seeding is permanently disabled once real usage exists.
func TestSeedUsage_ReplacementNotPeak(t *testing.T) {
	a := &Agent{id: MainAgentID}
	a.seedUsageEstimate(0, nil, 0)
	if a.LastUsage() != (fantasy.Usage{}) {
		t.Fatalf("zero estimate seeded: %+v", a.LastUsage())
	}

	a.seedUsageEstimate(50_000, nil, 0)
	if a.LastUsage().InputTokens != 50_000 {
		t.Fatalf("seed not stored: %+v", a.LastUsage())
	}

	// First real report is smaller than the seed: must replace, not peak.
	a.observeStepUsage(fantasy.Usage{InputTokens: 30_000}, nil, 0)
	if a.LastUsage().InputTokens != 30_000 {
		t.Fatalf("real report did not replace the seed: %+v", a.LastUsage())
	}
	// Subsequent steps peak as usual.
	a.observeStepUsage(fantasy.Usage{InputTokens: 40_000}, nil, 0)
	if a.LastUsage().InputTokens != 40_000 {
		t.Fatalf("peak rule broken after seed replacement: %+v", a.LastUsage())
	}
	// Seeding is permanently off after a real report.
	a.seedUsageEstimate(99_999, nil, 0)
	if a.LastUsage().InputTokens != 40_000 {
		t.Fatalf("re-seed overwrote real usage: %+v", a.LastUsage())
	}
}
