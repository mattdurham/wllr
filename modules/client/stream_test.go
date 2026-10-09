package client

// NOTE: Any changes to this file must be reflected in the corresponding SPECS.md or NOTES.md.

import (
	"bufio"
	"context"
	"io"
	"strings"
	"testing"

	"github.com/mattdurham/wllr/modules/wire"
)

// nopCloser wraps a reader as an io.ReadCloser for constructing a stream in a
// test without a real HTTP response.
type nopCloser struct{ io.Reader }

func (nopCloser) Close() error { return nil }

// newReader is a small convenienction for building the buffered reader a stream
// expects.
func newReader(r io.Reader) *bufio.Reader { return bufio.NewReader(r) }

// TestStreamSkipsHeartbeatsAndComments proves comment lines (heartbeats) and
// blank separators are ignored while data frames decode.
func TestStreamSkipsHeartbeatsAndComments(t *testing.T) {
	body := ": keep-alive\n\n" +
		"data: {\"seq\":1,\"kind\":\"token\",\"text\":\"hi\"}\n\n" +
		": keep-alive\n\n" +
		"data: {\"seq\":2,\"kind\":\"turn_done\"}\n\n"
	s := &stream{
		body:   nopCloser{strings.NewReader(body)},
		reader: newReader(strings.NewReader(body)),
	}

	ev, err := s.next()
	if err != nil || ev.Kind != wire.KindToken || ev.Text != "hi" || ev.Seq != 1 {
		t.Fatalf("first event = %+v, err=%v", ev, err)
	}
	ev, err = s.next()
	if err != nil || ev.Kind != wire.KindTurnDone || ev.Seq != 2 {
		t.Fatalf("second event = %+v, err=%v", ev, err)
	}
	// A third read hits EOF cleanly.
	if _, err := s.next(); err == nil {
		t.Error("expected EOF after the last frame")
	}
}

// TestStreamRecordsProtocolFromReady proves the ready frame's protocol version
// is captured for a compatibility gate.
func TestStreamRecordsProtocolFromReady(t *testing.T) {
	body := "data: {\"kind\":\"ready\",\"protocol\":1,\"seq\":9}\n\n"
	s := &stream{
		body:   nopCloser{strings.NewReader(body)},
		reader: newReader(strings.NewReader(body)),
	}
	if _, err := s.next(); err != nil {
		t.Fatalf("next: %v", err)
	}
	if s.protocol != 1 {
		t.Errorf("protocol = %d, want 1", s.protocol)
	}
}

// TestStreamRejectsBadJSON proves a malformed frame is an error, not a silently
// skipped line — the follower treats it as a fault and resynchronizes.
func TestStreamRejectsBadJSON(t *testing.T) {
	body := "data: {not json}\n\n"
	s := &stream{
		body:   nopCloser{strings.NewReader(body)},
		reader: newReader(strings.NewReader(body)),
	}
	if _, err := s.next(); err == nil {
		t.Fatal("expected a decode error")
	}
}

// TestOpenStreamMapsNon200ToHTTPError proves an auth failure surfaces as a typed
// error rather than an empty stream.
func TestOpenStreamMapsNon200ToHTTPError(t *testing.T) {
	f := newFakeInstance(t)
	f.tokenOK = "secret"
	c, err := New(Config{ID: "a", BaseURL: f.url()})
	if err != nil {
		t.Fatalf("New: %v", err)
	}
	_, err = c.openStream(context.Background(), 0)
	if err == nil {
		t.Fatal("expected an error")
	}
	if !IsUnauthorized(err) {
		t.Errorf("expected unauthorized, got %v", err)
	}
}
