//go:build wasip1

// Package main is the otel-traces extension for the wllr coding harness.
// It observes agent turns and tool calls via the event bus and exports them
// as OpenTelemetry traces to an OTLP HTTP endpoint using basic auth.
//
// Trace structure:
//   - One trace per session (random 16-byte trace ID)
//   - One parent span per agent turn (before_agent_start → message_end)
//   - One child span per tool call (point-in-time, no result captured)
//   - Maximum 100 tool-call spans per turn (excess silently dropped)
//
// Configuration (via config_read):
//
//	[extensions.otel-traces]
//	server   = "http://tempo:4318"   # base URL, /v1/traces is appended
//	username = "myuser"
//	token    = "mytoken"
package main

import (
	"encoding/base64"
	"encoding/json"
	"strings"
	"time"
)

// ─── Constants ────────────────────────────────────────────────────────────────

const (
	maxSpans         = 100 // maximum tool-call spans buffered per turn
	inputTruncateLen = 200 // max chars of tool input to capture
	promptPreviewLen = 100 // max chars of prompt to capture in turn span
)

// ─── Config ───────────────────────────────────────────────────────────────────

// Config is loaded from the host via config_read.
type Config struct {
	Server   string `json:"server"`   // OTLP HTTP base URL, e.g. "http://tempo:4318"
	Username string `json:"username"` // HTTP basic auth username
	Token    string `json:"token"`    // HTTP basic auth password / token
}

// ─── State ────────────────────────────────────────────────────────────────────

var (
	cfg         Config
	traceID     [16]byte
	spans       []spanRecord // child (tool-call) spans for the current turn
	currentTurn spanRecord   // the open parent span for this turn
	turnOpen    bool
	rng         uint64 // xorshift64 PRNG state
)

// ─── Random ID generation ─────────────────────────────────────────────────────
// We cannot use crypto/rand in wasip1 with wazero's PRNG, so we use a
// simple xorshift64 seeded from the nanosecond wall clock.

func xorshift64() uint64 {
	rng ^= rng << 13
	rng ^= rng >> 7
	rng ^= rng << 17
	return rng
}

func uint64BE(v uint64) []byte {
	return []byte{
		byte(v >> 56), byte(v >> 48), byte(v >> 40), byte(v >> 32),
		byte(v >> 24), byte(v >> 16), byte(v >> 8), byte(v),
	}
}

func newTraceID() [16]byte {
	var id [16]byte
	copy(id[:8], uint64BE(xorshift64()))
	copy(id[8:], uint64BE(xorshift64()))
	return id
}

func newSpanID() [8]byte {
	var id [8]byte
	copy(id[:], uint64BE(xorshift64()))
	return id
}

// ─── Init ─────────────────────────────────────────────────────────────────────

func init() {
	// Seed the PRNG from wall time. Zero is a degenerate xorshift64 seed.
	rng = uint64(time.Now().UnixNano())
	if rng == 0 {
		rng = 0xdeadbeefcafebabe
	}

	loadConfig()

	OnSessionStart(onSessionStart)
	OnBeforeAgentStart(onBeforeAgentStart)
	OnBeforeToolCall(onBeforeToolCall)
	OnMessageEnd(onMessageEnd)
}

// loadConfig reads extension configuration from the host.
func loadConfig() {
	raw := _sdkCallResult("config_read", map[string]string{})
	if raw == nil {
		Logf(1, "otel-traces: config_read returned no data, tracing disabled")
		return
	}
	s := strings.TrimSpace(string(raw))
	if s == "" || s == "{}" || s == "null" {
		Logf(1, "otel-traces: no server configured, tracing disabled")
		return
	}
	if err := json.Unmarshal(raw, &cfg); err != nil {
		Logf(3, "otel-traces: failed to parse config: %v", err)
		return
	}
	if cfg.Server == "" {
		Logf(1, "otel-traces: server field is empty, tracing disabled")
		return
	}
	Logf(1, "otel-traces: configured, exporting to %s", cfg.Server)
}

// ─── Event handlers ───────────────────────────────────────────────────────────

func onSessionStart() {
	traceID = newTraceID()
	spans = spans[:0]
	turnOpen = false
	Logf(0, "otel-traces: session started, new trace ID generated")
}

func onBeforeAgentStart(prompt string) {
	// If a previous turn was not closed (e.g. no message_end), discard it.
	if turnOpen {
		Logf(0, "otel-traces: closing unclosed turn span")
		spans = spans[:0]
		turnOpen = false
	}

	currentTurn = spanRecord{
		spanID:    newSpanID(),
		name:      "agent_turn",
		startNano: time.Now().UnixNano(),
		kind:      1, // INTERNAL
		attrs:     [][2]string{{"wllr.prompt.preview", truncate(prompt, promptPreviewLen)}},
	}
	spans = spans[:0]
	turnOpen = true
}

func onBeforeToolCall(payload json.RawMessage) {
	if !turnOpen {
		return
	}
	if len(spans) >= maxSpans {
		return
	}

	var p struct {
		AgentID  string          `json:"agent_id"`
		ToolName string          `json:"tool_name"`
		Input    json.RawMessage `json:"input"`
	}
	if err := json.Unmarshal(payload, &p); err != nil {
		return
	}

	now := time.Now().UnixNano()
	s := spanRecord{
		spanID:       newSpanID(),
		parentSpanID: currentTurn.spanID,
		name:         "tool:" + p.ToolName,
		startNano:    now,
		endNano:      now, // point-in-time: tool results are not captured
		kind:      1, // INTERNAL — tool calls are local dispatch, not remote calls
		attrs: [][2]string{
			{"agent.id", p.AgentID},
			{"tool.input", truncate(string(p.Input), inputTruncateLen)},
		},
	}
	spans = append(spans, s)
}

func onMessageEnd(role, content string) {
	if !turnOpen {
		return
	}
	turnOpen = false

	currentTurn.endNano = time.Now().UnixNano()
	currentTurn.attrs = append(currentTurn.attrs, [2]string{"wllr.role", role})

	// Build the full span list: parent turn first, then tool-call children.
	allSpans := make([]spanRecord, 0, len(spans)+1)
	allSpans = append(allSpans, currentTurn)
	allSpans = append(allSpans, spans...)
	spans = spans[:0]

	if cfg.Server != "" {
		sendTrace(allSpans)
	}
}

// ─── Trace export ─────────────────────────────────────────────────────────────

// sendTrace serialises allSpans into OTLP protobuf and POSTs it to the
// configured OTLP endpoint via the host http_post function.
func sendTrace(allSpans []spanRecord) {
	data := encodeTraceRequest(traceID, allSpans)
	if len(data) == 0 {
		return
	}

	url := strings.TrimRight(cfg.Server, "/") + "/v1/traces"

	headers := map[string]string{
		"Content-Type": "application/x-protobuf",
	}
	if cfg.Username != "" || cfg.Token != "" {
		creds := base64.StdEncoding.EncodeToString([]byte(cfg.Username + ":" + cfg.Token))
		headers["Authorization"] = "Basic " + creds
	}

	statusCode, _, err := HTTPPost(url, headers, data)
	if err != nil {
		Logf(3, "otel-traces: http_post failed: %v", err)
		return
	}
	if statusCode < 200 || statusCode >= 300 {
		Logf(2, "otel-traces: unexpected HTTP status %d exporting %d spans", statusCode, len(allSpans))
		return
	}
	Logf(0, "otel-traces: exported %d spans → HTTP %d", len(allSpans), statusCode)
}

// ─── Helpers ──────────────────────────────────────────────────────────────────

// truncate clips s to at most n runes, appending an ellipsis if truncated.
func truncate(s string, n int) string {
	r := []rune(s)
	if len(r) > n {
		return string(r[:n]) + "…"
	}
	return s
}

func main() {}
