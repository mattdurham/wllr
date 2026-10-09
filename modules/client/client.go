package client

// NOTE: Any changes to this file must be reflected in the corresponding SPECS.md or NOTES.md.

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"strings"
	"time"

	"github.com/mattdurham/wllr/modules/wire"
)

// DefaultHTTPTimeout bounds the REST helper requests. It does not apply to the
// event stream, which is long-lived and uses the caller's context instead.
const DefaultHTTPTimeout = 30 * time.Second

// Client talks to one wllr instance: REST helpers for history/submit/agents and
// a reconnecting SSE follower for the event stream. It is safe for concurrent
// use.
type Client struct {
	httpClient *http.Client
	id         string
	baseURL    string
	token      string
}

// New builds a Client for one instance. ID and BaseURL are required.
func New(cfg Config) (*Client, error) {
	id := strings.TrimSpace(cfg.ID)
	if id == "" {
		return nil, fmt.Errorf("client: instance ID is required")
	}
	base := strings.TrimRight(strings.TrimSpace(cfg.BaseURL), "/")
	if base == "" {
		return nil, fmt.Errorf("client: instance %q has no BaseURL", id)
	}
	hc := cfg.HTTPClient
	if hc == nil {
		hc = &http.Client{Timeout: DefaultHTTPTimeout}
	}
	return &Client{id: id, baseURL: base, token: cfg.Token, httpClient: hc}, nil
}

// ID returns the instance identifier.
func (c *Client) ID() string { return c.id }

// BaseURL returns the instance origin.
func (c *Client) BaseURL() string { return c.baseURL }

// authorize adds the bearer token when one is configured.
func (c *Client) authorize(req *http.Request) {
	if c.token != "" {
		req.Header.Set("Authorization", "Bearer "+c.token)
	}
}

// getJSON performs a GET and decodes the JSON body into out.
func (c *Client) getJSON(ctx context.Context, path string, out any) error {
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, c.baseURL+path, nil)
	if err != nil {
		return err
	}
	c.authorize(req)
	return c.do(req, out)
}

// postJSON performs a POST with a JSON body and decodes the response into out.
func (c *Client) postJSON(ctx context.Context, path string, body, out any) error {
	buf, err := json.Marshal(body)
	if err != nil {
		return err
	}
	req, err := http.NewRequestWithContext(
		ctx,
		http.MethodPost,
		c.baseURL+path,
		bytes.NewReader(buf),
	)
	if err != nil {
		return err
	}
	req.Header.Set("Content-Type", "application/json")
	c.authorize(req)
	return c.do(req, out)
}

// do executes req, maps a non-2xx status to *HTTPError, and decodes the body.
func (c *Client) do(req *http.Request, out any) error {
	resp, err := c.httpClient.Do(req)
	if err != nil {
		return err
	}
	defer func() { _ = resp.Body.Close() }()
	if resp.StatusCode < 200 || resp.StatusCode >= 300 {
		body, _ := io.ReadAll(io.LimitReader(resp.Body, 4096))
		return &HTTPError{Status: resp.StatusCode, Body: strings.TrimSpace(string(body))}
	}
	if out == nil {
		_, _ = io.Copy(io.Discard, resp.Body)
		return nil
	}
	return json.NewDecoder(resp.Body).Decode(out)
}

// Submit sends a prompt to an agent (empty agent means the root agent) and
// returns once the server has accepted it (HTTP 202). The turn's output arrives
// on the event stream.
func (c *Client) Submit(ctx context.Context, agent, content string) error {
	body := map[string]string{"agent": agent, "content": content}
	return c.postJSON(ctx, "/api/submit", body, nil)
}

// Cancel interrupts an agent's active turn.
func (c *Client) Cancel(ctx context.Context, agent string) error {
	return c.postJSON(ctx, "/api/cancel", map[string]string{"agent": agent}, nil)
}

// Callback routes a front-end selection (a picker item, a text input, an agent
// tree action) back to the instance's engine, using the callback id from the
// corresponding stream frame.
func (c *Client) Callback(ctx context.Context, callback string, args ...string) error {
	body := map[string]any{"callback": callback}
	if len(args) == 1 {
		body["value"] = args[0]
	} else if len(args) > 1 {
		body["args"] = args
	}
	return c.postJSON(ctx, "/api/ui/callback", body, nil)
}

// ProtocolVersion is the wire-protocol version this client speaks, re-exported
// so callers do not need a second import for the version constant.
const ProtocolVersion = wire.ProtocolVersion
