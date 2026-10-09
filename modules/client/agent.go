package client

// NOTE: Any changes to this file must be reflected in the corresponding SPECS.md or NOTES.md.

import "context"

// Agent is one entry in the GET /api/agents response.
type Agent struct {
	ID          string `json:"id"`
	Name        string `json:"name,omitempty"`
	ActiveTool  string `json:"active_tool,omitempty"`
	LastTool    string `json:"last_tool,omitempty"`
	Inbox       int    `json:"inbox"`
	HistoryLen  int    `json:"history_len"`
	Compactions int    `json:"compactions"`
	Running     bool   `json:"running"`
	Main        bool   `json:"main"`
}

// Agents lists the instance's agents.
func (c *Client) Agents(ctx context.Context) ([]Agent, error) {
	var out struct {
		Agents []Agent `json:"agents"`
	}
	err := c.getJSON(ctx, "/api/agents", &out)
	return out.Agents, err
}
