package client

// NOTE: Any changes to this file must be reflected in the corresponding SPECS.md or NOTES.md.

import (
	"bufio"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"strconv"
	"strings"

	"github.com/mattdurham/wllr/modules/wire"
)

// stream drives one instance's Server-Sent Events connection. It is the
// transport half of a Client: it knows how to open /api/stream, skip
// heartbeats, and decode `data:` frames into wire.Events. It holds no state
// beyond the current response, so a reconnect is simply a new stream.
type stream struct {
	body   io.ReadCloser
	reader *bufio.Reader
	// protocol is the version from the first ready frame, or 0 if not yet seen.
	protocol int
}

// openStream issues GET /api/stream with an optional `since` cursor and returns
// a stream positioned after the response headers. A non-200 status is an error
// carrying the response body, so an auth failure surfaces as 401 rather than a
// silent zero-value stream.
func (c *Client) openStream(ctx context.Context, since int64) (*stream, error) {
	u := c.baseURL + "/api/stream"
	if since > 0 {
		u += "?since=" + strconv.FormatInt(since, 10)
	}
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, u, nil)
	if err != nil {
		return nil, err
	}
	req.Header.Set("Accept", "text/event-stream")
	c.authorize(req)

	resp, err := c.httpClient.Do(req)
	if err != nil {
		return nil, err
	}
	if resp.StatusCode != http.StatusOK {
		body, _ := io.ReadAll(io.LimitReader(resp.Body, 4096))
		_ = resp.Body.Close()
		return nil, &HTTPError{Status: resp.StatusCode, Body: strings.TrimSpace(string(body))}
	}
	return &stream{body: resp.Body, reader: bufio.NewReader(resp.Body)}, nil
}

// next reads the next event frame, skipping comment/heartbeat lines and blank
// separators. It returns io.EOF when the server closes the stream cleanly. A
// frame whose data does not decode as JSON is reported so the caller can decide
// whether to treat it as a gap-triggering fault (it does).
func (s *stream) next() (wire.Event, error) {
	var data []byte
	for {
		line, err := s.reader.ReadString('\n')
		if err != nil {
			if err == io.EOF && len(data) > 0 {
				// A final frame without a trailing blank line still counts.
				return s.decode(data)
			}
			return wire.Event{}, err
		}
		line = strings.TrimRight(line, "\r\n")
		switch {
		case line == "":
			// Blank line terminates a frame. If we have data, emit it.
			if len(data) > 0 {
				return s.decode(data)
			}
			continue
		case strings.HasPrefix(line, ":"):
			// Comment (heartbeat): ignore.
			continue
		case strings.HasPrefix(line, "data:"):
			chunk := strings.TrimPrefix(line, "data:")
			chunk = strings.TrimPrefix(chunk, " ")
			if len(data) > 0 {
				data = append(data, '\n')
			}
			data = append(data, chunk...)
		default:
			// Unknown SSE field (event:, id:, retry:): ignored, matching the
			// server, which only ever writes data: and : comment lines.
			continue
		}
	}
}

// decode unmarshals one frame's data and records the protocol version from a
// ready frame.
func (s *stream) decode(data []byte) (wire.Event, error) {
	var ev wire.Event
	if err := json.Unmarshal(data, &ev); err != nil {
		return wire.Event{}, fmt.Errorf("client: bad stream frame: %w", err)
	}
	if ev.Kind == wire.KindReady {
		s.protocol = ev.Protocol
	}
	return ev, nil
}

// Close releases the connection. It is safe to call more than once.
func (s *stream) Close() error {
	if s.body == nil {
		return nil
	}
	return s.body.Close()
}
