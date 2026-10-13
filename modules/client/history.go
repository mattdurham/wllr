package client

// NOTE: Any changes to this file must be reflected in the corresponding SPECS.md or NOTES.md.

import (
	"context"
	"net/url"
	"strconv"
)

// History is the GET /api/history response.
type History struct {
	AgentID  string    `json:"agent_id"`
	Messages []Message `json:"messages"`
	Total    int       `json:"total"`
	Offset   int       `json:"offset"`
}

// History fetches an agent's transcript. Every message carries an HTML body the
// server rendered, so a caller does not need its own markdown/ANSI renderer.
func (c *Client) History(ctx context.Context, opts HistoryOptions) (History, error) {
	q := "?"
	if opts.Agent != "" {
		q += "agent=" + url.QueryEscape(opts.Agent) + "&"
	}
	if opts.Offset > 0 {
		q += "offset=" + strconv.Itoa(opts.Offset) + "&"
	}
	if opts.Limit > 0 {
		q += "limit=" + strconv.Itoa(opts.Limit) + "&"
	}
	var h History
	err := c.getJSON(ctx, "/api/history"+q, &h)
	return h, err
}
