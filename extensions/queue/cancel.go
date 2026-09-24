package main

// Queue-tool request parsing and the ownership rule for queue_peek and
// queue_cancel. This file has no build tag so its logic is testable natively
// (go test ./extensions/queue); the wasip1-tagged main.go supplies the ABI.

import (
	"encoding/json"
	"fmt"
	"strings"
)

// beforeToolCallPayload mirrors the before_tool_call event the host dispatches.
type beforeToolCallPayload struct {
	AgentID    string          `json:"agent_id"`
	ToolCallID string          `json:"tool_call_id"`
	ToolName   string          `json:"tool_name"`
	Input      json.RawMessage `json:"input"`
}

// queueToolRequest is the shared input shape of queue_peek and queue_cancel.
// Index and MessageID are mutually exclusive single-message selectors; when
// neither is set, queue_cancel discards the whole queue.
type queueToolRequest struct {
	AgentID   string `json:"agent_id"`
	Index     *int   `json:"index"`
	MessageID string `json:"message_id"`
}

// cancelPlan is a validated queue_cancel request: the target agent, whether the
// whole queue is cleared, and the single-message selector when it is not.
type cancelPlan struct {
	Target      string
	Clear       bool
	ByIndex     *int
	ByMessageID string
}

// parseQueueToolRequest unmarshals and validates tool input.
func parseQueueToolRequest(raw json.RawMessage) (queueToolRequest, error) {
	var req queueToolRequest
	if len(raw) > 0 {
		if err := json.Unmarshal(raw, &req); err != nil {
			return req, fmt.Errorf("invalid input: %v", err)
		}
	}
	if req.Index != nil && req.MessageID != "" {
		return req, fmt.Errorf("index and message_id are mutually exclusive")
	}
	if req.Index != nil && *req.Index < 0 {
		return req, fmt.Errorf("index must be >= 0")
	}
	return req, nil
}

// resolveQueueTarget applies the queue ownership rule: an agent may inspect or
// cancel its own queue or a descendant's queue ("<caller>/..."), never another
// agent's — a sub-agent must not be able to discard the orchestrator's queued
// user messages. An empty caller is the root agent ("main" by the spawn
// convention); every spawned ID is rooted there, so the descendant prefix check
// already covers the whole tree for the root.
func resolveQueueTarget(caller, requested string) (string, bool) {
	if caller == "" {
		caller = "main"
	}
	if requested == "" {
		return caller, true
	}
	if requested == caller || strings.HasPrefix(requested, caller+"/") {
		return requested, true
	}
	return "", false
}

// planCancel validates the request against the caller and returns the host-call
// plan. The default (no selector) clears the target's whole queue.
func planCancel(caller string, req queueToolRequest) (cancelPlan, error) {
	target, ok := resolveQueueTarget(caller, req.AgentID)
	if !ok {
		return cancelPlan{}, fmt.Errorf("agent_id %q is not you or one of your sub-agents", req.AgentID)
	}
	plan := cancelPlan{Target: target}
	switch {
	case req.Index != nil:
		plan.ByIndex = req.Index
	case req.MessageID != "":
		plan.ByMessageID = req.MessageID
	default:
		plan.Clear = true
	}
	return plan, nil
}

// unwrapHostCallEnvelope extracts the Result payload from a host_call reply.
// The host replies with a host_call envelope ({result: ...} or {error: ...}),
// not the payload itself — parsing the envelope directly made every /queue
// command decode to zero values (the same bug the agents extension hit with
// agent_list). An error field returns as a Go error; an empty Result with no
// error is a legitimate success (e.g. mailbox_edit returns no body).
func unwrapHostCallEnvelope(raw []byte) (string, error) {
	var envelope struct {
		Error  string          `json:"error,omitempty"`
		Result json.RawMessage `json:"result,omitempty"`
	}
	if err := json.Unmarshal(raw, &envelope); err != nil {
		return "", fmt.Errorf("host returned malformed response: %v", err)
	}
	if envelope.Error != "" {
		return "", fmt.Errorf("%s", envelope.Error)
	}
	return string(envelope.Result), nil
}
