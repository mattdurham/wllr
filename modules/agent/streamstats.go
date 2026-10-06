package agent

// NOTE: Any changes to this file must be reflected in the corresponding SPECS.md or NOTES.md.

import (
	"strings"
	"sync"
	"time"
)

// minStreamSpan bounds the denominator when converting streamed output into a
// tokens-per-second rate. Below this the quotient is noise: a fake or instant
// stream delivering all deltas within microseconds would otherwise report
// absurd speeds.
const minStreamSpan = 100 * time.Millisecond

// liveCharsPerToken is the chars-per-token estimate for the live (mid-turn)
// rate, matching the chars/4 estimation convention used elsewhere
// (estimateFantasyMessageBytes, proactive compaction). Only the in-flight
// display uses it; the frozen end-of-turn rate uses provider-reported
// OutputTokens.
const liveCharsPerToken = 4

// sparkWindow is the trailing window over which each sparkline sample's rate
// is measured. One second matches the display's "t/s" unit and smooths the
// ~75ms token-batching pulses into a readable shape.
const sparkWindow = time.Second

// sparkPoints is how many one-second samples the sparkline keeps — the
// trailing ~20 seconds of generation activity.
const sparkPoints = 20

// tokSample records the cumulative streamed character count at a token
// arrival, the raw material for the trailing-window rate.
type tokSample struct {
	at    time.Time
	chars int64
}

// streamStats accumulates provider-stream timing for one agent turn so the
// statusline can show generation speed (tokens/second). Spans follow fantasy's
// step lifecycle: OnStepStart opens a span just before each provider call
// (so time-to-first-token is included), OnTextDelta marks token arrivals and
// accumulates characters, and OnStepFinish closes the span right after the
// stream ends — before fantasy executes the step's tools. Tool execution and
// sub-agent wait time therefore fall between spans and are excluded, which is
// the whole point: tokens/second measures generation speed, not turn duration.
//
// All mutating methods take explicit timestamps (callers pass time.Now()) so
// tests are deterministic. A streamStats is owned by one turn, but its live
// rate is read from the UI goroutine, so every method takes the internal lock.
type streamStats struct {
	mu sync.Mutex

	// spanStart is when the currently open provider-call span began. Zero when
	// no span is open (before the first step, and after finish).
	spanStart time.Time
	// lastTokenAt is the arrival time of the most recent text delta in the
	// open span. Zero inside a span that has streamed nothing yet (TTFT window).
	lastTokenAt time.Time
	// spanChars counts text-delta characters in the open span for the live
	// chars/4 estimate. Reset when the span closes — the step's reported
	// output tokens supersede the estimate, and carrying the chars forward
	// would double-count them in live.
	spanChars int64
	// totalChars counts every streamed character across all spans and is
	// never reset by the step lifecycle. finish uses it as the output
	// estimate when the provider reported no usage at all.
	totalChars int64
	// doneOutput is the OutputTokens reported by completed steps, used by the
	// live rate until the authoritative total arrives at finish.
	doneOutput int64
	// streamedNS is the accumulated duration of closed spans.
	streamedNS time.Duration
	// finished marks that finish has run; after it, live returns lastTps.
	finished bool
	// lastTps is the frozen end-of-turn rate.
	lastTps float64

	// Sparkline state (all guarded by mu). samples records cumulative
	// totalChars at token arrivals for the trailing-window rate; hist holds
	// at most sparkPoints one-second windowed rates, oldest first;
	// lastSampleAt is when hist last grew (sample cadence); lastSpark is the
	// bars frozen at finish and shown while idle.
	samples      []tokSample
	hist         []float64
	lastSampleAt time.Time
	lastSpark    string
}

func newStreamStats() *streamStats { return &streamStats{} }

// stepStart opens a new stream span. Any still-open span is closed first
// (defensive: OnStepFinish should have closed it).
func (s *streamStats) stepStart(now time.Time) {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.closeSpanLocked()
	s.spanStart = now
	s.lastTokenAt = time.Time{}
	s.spanChars = 0
}

// stepFinish closes the open span and records the completed step's output
// tokens for the live rate. The span's char estimate is dropped: the step's
// reported output tokens supersede it.
func (s *streamStats) stepFinish(now time.Time, stepOutputTokens int64) {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.closeSpanLocked()
	s.spanChars = 0
	s.doneOutput += stepOutputTokens
}

// token records a text delta at now. Reopens a span if one is not open
// (defensive against providers whose callbacks arrive outside the step
// lifecycle).
func (s *streamStats) token(now time.Time, text string) {
	s.mu.Lock()
	defer s.mu.Unlock()
	if s.spanStart.IsZero() {
		s.spanStart = now
	}
	s.lastTokenAt = now
	s.spanChars += int64(len(text))
	s.totalChars += int64(len(text))
	s.samples = append(s.samples, tokSample{at: now, chars: s.totalChars})
	// Keep enough history to anchor the window (one sample at or before its
	// start so the delta can span the boundary); anything older can never
	// contribute again.
	cutoff := now.Add(-2 * sparkWindow)
	drop := 0
	for drop < len(s.samples) && s.samples[drop].at.Before(cutoff) {
		drop++
	}
	if drop > 0 {
		s.samples = append(s.samples[:0], s.samples[drop:]...)
	}
}

// finish closes any open span and freezes the exact end-of-turn rate from the
// authoritative total output. reported is false when the turn failed or the
// provider reported no usage, in which case the best available output estimate
// stands in (max of the per-step reported sum and the chars/4 estimate over
// all streamed text). A turn that streamed nothing reports rate 0.
func (s *streamStats) finish(now time.Time, totalOutputTokens int64, reported bool) {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.closeSpanLocked()
	s.spanStart = time.Time{}
	output := s.doneOutput
	if reported && totalOutputTokens > output {
		output = totalOutputTokens
	}
	if !reported {
		// No usage at all (failed turn or silent provider): the best available
		// estimate is chars/4 over everything streamed. max() keeps a partially
		// reported turn from double-counting — the estimate covers all spans,
		// doneOutput only the reported ones, so never add them.
		if est := s.totalChars / liveCharsPerToken; est > output {
			output = est
		}
	}
	s.doneOutput = output
	// Fold once: endStreamStats may legitimately run twice (normal exit plus
	// the deferred panic guard) and a second finish must not re-add the
	// estimate.
	s.spanChars = 0
	s.lastSpark = sparkBars(s.hist)
	s.finished = true
	s.lastTps = tpsRate(output, s.streamedNS)
}

// live reports the current rate: the frozen exact rate once finished, else the
// live rate (completed-step output plus a chars/4 estimate for the open span,
// over accumulated streaming time). Returns 0 while there is nothing to show —
// before the first token of the first step, and whenever the accumulated
// streaming time is below minStreamSpan.
func (s *streamStats) live(now time.Time) float64 {
	s.mu.Lock()
	defer s.mu.Unlock()
	if s.finished {
		return s.lastTps
	}
	output := s.doneOutput + s.spanChars/liveCharsPerToken
	ns := s.streamedNS
	if !s.spanStart.IsZero() && s.lastTokenAt.After(s.spanStart) {
		ns += s.lastTokenAt.Sub(s.spanStart)
	}
	return tpsRate(output, ns)
}

// exact returns the frozen end-of-turn rate (0 until finish has run).
func (s *streamStats) exact() float64 {
	s.mu.Lock()
	defer s.mu.Unlock()
	return s.lastTps
}

// spark appends at most one windowed-rate sample per sparkWindow of poll time
// and renders the statusline sparkline bars. The UI tick calls it (~10Hz)
// while a turn streams; sampling happens only while a span is open with at
// least one token, so between spans (tool execution, sub-agent waits) the
// bars freeze rather than recording inactivity — matching the number's
// span-based accounting. After finish it returns the frozen bars.
func (s *streamStats) spark(now time.Time) string {
	s.mu.Lock()
	defer s.mu.Unlock()
	if s.finished {
		return s.lastSpark
	}
	if s.spanStart.IsZero() || !s.lastTokenAt.After(s.spanStart) {
		// No open span, or a span whose first token has not arrived (TTFT):
		// history renders as-is without new samples.
		return sparkBars(s.hist)
	}
	rate := s.windowedRateLocked(now)
	if s.lastSampleAt.IsZero() || now.Sub(s.lastSampleAt) >= sparkWindow {
		s.hist = append(s.hist, rate)
		if len(s.hist) > sparkPoints {
			s.hist = s.hist[len(s.hist)-sparkPoints:]
		}
		s.lastSampleAt = now
	}
	// Render history plus the current rate so the leading edge moves at the
	// tick cadence rather than once per second.
	return sparkBars(append(s.hist, rate))
}

// sparkText returns the bars as they stand (the frozen bars after finish).
// endStreamStats uses it to freeze the display onto the agent.
func (s *streamStats) sparkText() string {
	s.mu.Lock()
	defer s.mu.Unlock()
	if s.finished {
		return s.lastSpark
	}
	return sparkBars(s.hist)
}

// windowedRateLocked computes tokens/second over the trailing sparkWindow of
// token arrivals, via the chars/4 live estimate. The measured span starts at
// the later of the window start and the span's beginning, so a span that
// just opened is not deflated by an otherwise-empty window.
func (s *streamStats) windowedRateLocked(now time.Time) float64 {
	if len(s.samples) == 0 {
		return 0
	}
	start := now.Add(-sparkWindow)
	if s.spanStart.After(start) {
		start = s.spanStart
	}
	base := int64(0)
	measuredFrom := start
	for _, smp := range s.samples {
		if smp.at.After(start) {
			break
		}
		base = smp.chars
		measuredFrom = smp.at
	}
	if measuredFrom.Before(start) {
		// The newest at-or-before-start sample is stale (window slid past it):
		// coverage is the full window; its chars arrived before start and are
		// excluded from the delta anyway.
		measuredFrom = start
	}
	tokens := (s.totalChars - base) / liveCharsPerToken
	return tpsRate(tokens, now.Sub(measuredFrom))
}

// sparkBars renders rates as unicode block bars, scaled to the window's max
// so the shape stays readable regardless of absolute speed.
func sparkBars(rates []float64) string {
	if len(rates) == 0 {
		return ""
	}
	blocks := []rune("▁▂▃▄▅▆▇█")
	max := rates[0]
	for _, r := range rates[1:] {
		if r > max {
			max = r
		}
	}
	var b strings.Builder
	for _, r := range rates {
		idx := 0
		if max > 0 {
			idx = int(r/max*float64(len(blocks)-1) + 0.5)
			if idx > len(blocks)-1 {
				idx = len(blocks) - 1
			}
		}
		b.WriteRune(blocks[idx])
	}
	return b.String()
}

// closeSpanLocked folds the open span into streamedNS exactly once and
// closes it (zeroing span state so neither live nor a later close can fold
// it again). Callers hold mu. The span contributes only up to the last
// token: a stalled stream's silent tail is TTFT-of-nothing, not generation
// time.
func (s *streamStats) closeSpanLocked() {
	if s.spanStart.IsZero() {
		return
	}
	if s.lastTokenAt.After(s.spanStart) {
		s.streamedNS += s.lastTokenAt.Sub(s.spanStart)
	}
	s.spanStart = time.Time{}
	s.lastTokenAt = time.Time{}
}

// tpsRate converts output tokens over a streaming duration into a rate.
// Zero (hide the segment) when there are no tokens or the duration is below
// minStreamSpan.
func tpsRate(outputTokens int64, streamed time.Duration) float64 {
	if outputTokens <= 0 || streamed < minStreamSpan {
		return 0
	}
	return float64(outputTokens) / streamed.Seconds()
}
