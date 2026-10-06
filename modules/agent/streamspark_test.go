package agent

// NOTE: Any changes to this file must be reflected in the corresponding SPECS.md or NOTES.md.

import (
	"strings"
	"testing"
)

// TestSpark_ConstantRateBarsEqual feeds a steady 25 tokens/second stream and
// verifies every bar is at full height (each sample scales to the window max).
func TestSpark_ConstantRateBarsEqual(t *testing.T) {
	s := newStreamStats()
	s.stepStart(at(0))
	// Seed a sample at the span start so the first window measures a full
	// second instead of deflating over the TTFT-style partial window.
	s.token(at(0), strings.Repeat("abcd", 25)) // 100 chars = 25 tokens
	var got string
	for ms := 1000; ms <= 5000; ms += 1000 {
		s.token(at(ms), strings.Repeat("abcd", 25))
		got = s.spark(at(ms))
	}
	// 5 stored samples + the transient leading edge = 6 bars, left-padded
	// with sparkPad to the constant 8-slot frame (width never grows).
	if want := "··██████"; got != want {
		t.Errorf("spark constant rate = %q, want %q", got, want)
	}

	// The frame is full-width from the very first sample: the first poll
	// already renders sparkPoints columns, so the statusline never shifts.
	s2 := newStreamStats()
	s2.stepStart(at(0))
	s2.token(at(500), strings.Repeat("abcd", 25)) // strictly after span start
	if got := s2.spark(at(1000)); runeCount(got) != sparkPoints {
		t.Errorf("spark width after first sample = %d, want %d (constant frame)", runeCount(got), sparkPoints)
	}
}

// TestSpark_TTFTRecordsZeros verifies wall-clock sampling: from the very
// first poll — even before a span opens, and through the TTFT wait — zero
// samples accrue each second, so the bars are live from turn start instead
// of freezing. Whether the segment is visible at all is the harness's
// decision (formatTpsLive hides the segment while the number is zero); the
// tracker's job is only to stay current.
func TestSpark_TTFTRecordsZeros(t *testing.T) {
	s := newStreamStats()
	// First poll samples immediately: one zero in history plus the transient
	// leading edge = two bars in the constant frame.
	if got, want := s.spark(at(500)), "······▁▁"; got != want {
		t.Errorf("spark on fresh tracker = %q, want %q", got, want)
	}
	s.stepStart(at(1000))
	// One silent second later the second zero lands and everything shifts left.
	if got, want := s.spark(at(1500)), "·····▁▁▁"; got != want {
		t.Errorf("spark during TTFT = %q, want %q", got, want)
	}
}

// TestSpark_CadenceAndCap verifies one stored sample per second of polling
// (not one per 100ms tick) and that the render holds the constant sparkPoints
// frame no matter how long the turn runs.
func TestSpark_CadenceAndCap(t *testing.T) {
	s := newStreamStats()
	s.stepStart(at(0))
	s.token(at(0), strings.Repeat("abcd", 25))
	for ms := 1000; ms <= 30000; ms += 1000 {
		s.token(at(ms), strings.Repeat("abcd", 25))
		s.spark(at(ms))
	}
	// 30 seconds of samples, but the render is the fixed sparkPoints-wide
	// frame: history has long stopped growing.
	if got, want := s.spark(at(30000)), sparkPoints; runeCount(got) != want {
		t.Errorf("spark length = %d, want %d (constant frame)", runeCount(got), want)
	}
	if text := s.sparkText(); runeCount(text) != sparkPoints {
		t.Errorf("frozen bars length = %d, want %d (no transient edge)", runeCount(text), sparkPoints)
	}
}

// TestSpark_StallShowsDip verifies a silent second records a zero-rate
// sample: the dip is the visual signature of a stall.
func TestSpark_StallShowsDip(t *testing.T) {
	s := newStreamStats()
	s.stepStart(at(0))
	s.token(at(0), strings.Repeat("x", 200))   // 50 tokens (window-start anchor)
	s.token(at(500), strings.Repeat("x", 200)) // 50 more inside the first window
	s.spark(at(1000))                          // sample 1: 50 tokens over 1s = 50 t/s
	s.spark(at(2000))                          // sample 2: empty window → 0
	if got, want := s.spark(at(2500)), "·····█▁▁"; got != want {
		t.Errorf("spark after stall = %q, want %q", got, want)
	}
}

// TestSpark_ScrollsDuringToolGap verifies wall-clock sampling across a tool
// gap: with the span closed and no tokens arriving, each silent second
// records a zero sample, so the generation bar scrolls left and stall bars
// fill in — a live trailing window rather than a frozen frame.
func TestSpark_ScrollsDuringToolGap(t *testing.T) {
	s := newStreamStats()
	s.stepStart(at(0))
	s.token(at(0), strings.Repeat("x", 200))   // 50 tokens (window-start anchor)
	s.token(at(500), strings.Repeat("x", 200)) // 50 more inside the first window
	s.spark(at(1000))                          // sample 1: 50 tokens over 1s = 50 t/s
	s.stepFinish(at(1500), 50)                 // span closes; tool execution begins
	// Inside the same sampling second the leading edge already reads the
	// gap's zero; no stored sample yet.
	if got, want := s.spark(at(1600)), "······█▁"; got != want {
		t.Fatalf("spark at gap start = %q, want %q", got, want)
	}
	// Each silent second appends a zero sample and the bar scrolls left.
	want := []string{"·····█▁▁", "····█▁▁▁", "···█▁▁▁▁"}
	for i, ms := range []int{2000, 3000, 4000} {
		if got := s.spark(at(ms)); got != want[i] {
			t.Fatalf("spark at %dms = %q, want %q", ms, got, want[i])
		}
	}
	// After enough silent seconds every generation bar has scrolled off:
	// the frame is entirely stall bars, still exactly sparkPoints wide.
	for ms := 5000; ms <= 9000; ms += 1000 {
		s.spark(at(ms))
	}
	if got, want := s.spark(at(10000)), "▁▁▁▁▁▁▁▁"; got != want {
		t.Errorf("spark after 9 silent seconds = %q, want %q", got, want)
	}
}

// TestSpark_FinishFreezesBars verifies the bars survive turn end unchanged,
// mirroring the frozen number, and that live polling after finish is stable.
func TestSpark_FinishFreezesBars(t *testing.T) {
	s := newStreamStats()
	s.stepStart(at(0))
	s.token(at(0), strings.Repeat("abcd", 25))
	s.spark(at(1000))
	s.token(at(1000), strings.Repeat("abcd", 25))
	s.spark(at(2000))
	s.finish(at(2100), 50, true)
	want := s.sparkText()
	if want == "" {
		t.Fatal("sparkText empty after two sampled seconds")
	}
	if got := s.spark(at(90000)); got != want {
		t.Errorf("spark long after finish = %q, want frozen %q", got, want)
	}
}

// TestSpark_NoPollNoBars verifies a turn that ends before any poll follows
// its first token freezes no bars — the number alone shows for such turns.
func TestSpark_NoPollNoBars(t *testing.T) {
	s := newStreamStats()
	s.stepStart(at(0))
	s.token(at(500), strings.Repeat("abcd", 25))
	s.finish(at(700), 25, true)
	if got := s.sparkText(); got != "" {
		t.Errorf("sparkText with no post-token poll = %q, want \"\"", got)
	}
}

// TestAgent_StreamTpsSpark_Lifecycle exercises the agent-level sparkline
// lifecycle: empty before any turn, live bars while the tracker is open, and
// bars frozen by endStreamStats exactly as the tracker holds them.
func TestAgent_StreamTpsSpark_Lifecycle(t *testing.T) {
	a := &Agent{}
	if got := a.StreamTpsSpark(); got != "" {
		t.Errorf("StreamTpsSpark on fresh agent = %q, want \"\"", got)
	}

	stats := a.beginStreamStats()
	stats.stepStart(at(0))
	stats.token(at(0), strings.Repeat("x", 400)) // 100 tokens
	stats.spark(at(1000))
	stats.spark(at(2000))

	a.endStreamStats(100, true)
	if a.liveStats != nil {
		t.Error("liveStats not cleared by endStreamStats")
	}
	if want := stats.sparkText(); a.StreamTpsSpark() != want {
		t.Errorf("StreamTpsSpark after endStreamStats = %q, want frozen %q", a.StreamTpsSpark(), want)
	}

	// The deferred panic-guard call must not disturb the frozen bars.
	a.endStreamStats(0, false)
	if got := a.StreamTpsSpark(); got != stats.sparkText() {
		t.Errorf("StreamTpsSpark after redundant endStreamStats = %q, want %q", got, stats.sparkText())
	}
}

// TestPool_MainAgentTpsSpark_NoMain verifies the pool accessor degrades to ""
// when the main agent does not exist, matching MainAgentTps's zero.
func TestPool_MainAgentTpsSpark_NoMain(t *testing.T) {
	pool := NewPool()
	if got := pool.MainAgentTpsSpark(); got != "" {
		t.Errorf("MainAgentTpsSpark without main agent = %q, want \"\"", got)
	}
}

// runeCount counts runes without importing utf8 just for the tests.
func runeCount(s string) int { return len([]rune(s)) }
