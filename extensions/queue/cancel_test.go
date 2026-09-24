package main

import (
	"encoding/json"
	"testing"
)

func TestParseQueueToolRequest(t *testing.T) {
	tests := []struct {
		name    string
		input   string
		want    queueToolRequest
		wantErr bool
	}{
		{
			name:  "empty input defaults to own queue, full clear",
			input: "",
			want:  queueToolRequest{},
		},
		{
			name:  "empty object",
			input: `{}`,
			want:  queueToolRequest{},
		},
		{
			name:  "agent_id only",
			input: `{"agent_id":"main/coder"}`,
			want:  queueToolRequest{AgentID: "main/coder"},
		},
		{
			name:  "index selector",
			input: `{"index":2}`,
			want:  queueToolRequest{Index: intPtr(2)},
		},
		{
			name:  "message_id selector",
			input: `{"message_id":"m-123"}`,
			want:  queueToolRequest{MessageID: "m-123"},
		},
		{
			name:    "index and message_id together",
			input:   `{"index":1,"message_id":"m-123"}`,
			wantErr: true,
		},
		{
			name:    "negative index",
			input:   `{"index":-1}`,
			wantErr: true,
		},
		{
			name:    "malformed json",
			input:   `{"index":`,
			wantErr: true,
		},
		{
			name:    "wrong type for index",
			input:   `{"index":"2"}`,
			wantErr: true,
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got, err := parseQueueToolRequest(json.RawMessage(tt.input))
			if (err != nil) != tt.wantErr {
				t.Fatalf("parseQueueToolRequest(%q) error = %v, wantErr %v", tt.input, err, tt.wantErr)
			}
			if tt.wantErr {
				return
			}
			if got.AgentID != tt.want.AgentID || got.MessageID != tt.want.MessageID {
				t.Fatalf("got %+v, want %+v", got, tt.want)
			}
			if tt.want.Index == nil {
				if got.Index != nil {
					t.Fatalf("got index %d, want none", *got.Index)
				}
			} else {
				if got.Index == nil || *got.Index != *tt.want.Index {
					t.Fatalf("got index %v, want %d", got.Index, *tt.want.Index)
				}
			}
		})
	}
}

func TestResolveQueueTarget(t *testing.T) {
	tests := []struct {
		name      string
		caller    string
		requested string
		want      string
		wantOK    bool
	}{
		{name: "empty caller and request defaults to main", caller: "", requested: "", want: "main", wantOK: true},
		{name: "empty caller resolves root", caller: "", requested: "main", want: "main", wantOK: true},
		{
			name:      "empty caller is root and may target anything",
			caller:    "",
			requested: "main/a/b",
			want:      "main/a/b",
			wantOK:    true,
		},
		{name: "own queue", caller: "main/coder", requested: "", want: "main/coder", wantOK: true},
		{name: "explicit self", caller: "main/coder", requested: "main/coder", want: "main/coder", wantOK: true},
		{name: "descendant", caller: "main/coder", requested: "main/coder/lsp", want: "main/coder/lsp", wantOK: true},
		{
			name:      "deep descendant",
			caller:    "main/coder",
			requested: "main/coder/a/b",
			want:      "main/coder/a/b",
			wantOK:    true,
		},
		{name: "sibling denied", caller: "main/coder", requested: "main/reviewer", want: "", wantOK: false},
		{name: "orchestrator denied from sub-agent", caller: "main/coder", requested: "main", want: "", wantOK: false},
		{
			name:      "prefix without slash boundary denied",
			caller:    "main/coder",
			requested: "main/coderx",
			want:      "",
			wantOK:    false,
		},
		{name: "unrelated id denied", caller: "main/coder", requested: "other", want: "", wantOK: false},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got, ok := resolveQueueTarget(tt.caller, tt.requested)
			if ok != tt.wantOK || got != tt.want {
				t.Fatalf("resolveQueueTarget(%q, %q) = (%q, %v), want (%q, %v)",
					tt.caller, tt.requested, got, ok, tt.want, tt.wantOK)
			}
		})
	}
}

func TestPlanCancel(t *testing.T) {
	t.Run("default clears own queue", func(t *testing.T) {
		plan, err := planCancel("main/coder", queueToolRequest{})
		if err != nil {
			t.Fatalf("planCancel: %v", err)
		}
		if !plan.Clear || plan.Target != "main/coder" || plan.ByIndex != nil || plan.ByMessageID != "" {
			t.Fatalf("unexpected plan: %+v", plan)
		}
	})
	t.Run("index cancels one message from descendant", func(t *testing.T) {
		plan, err := planCancel("main", queueToolRequest{AgentID: "main/coder", Index: intPtr(1)})
		if err != nil {
			t.Fatalf("planCancel: %v", err)
		}
		if plan.Clear || plan.Target != "main/coder" || plan.ByIndex == nil || *plan.ByIndex != 1 {
			t.Fatalf("unexpected plan: %+v", plan)
		}
	})
	t.Run("message_id cancels one message", func(t *testing.T) {
		plan, err := planCancel("", queueToolRequest{MessageID: "m-9"})
		if err != nil {
			t.Fatalf("planCancel: %v", err)
		}
		if plan.Clear || plan.ByMessageID != "m-9" || plan.Target != "main" {
			t.Fatalf("unexpected plan: %+v", plan)
		}
	})
	t.Run("foreign agent denied", func(t *testing.T) {
		_, err := planCancel("main/coder", queueToolRequest{AgentID: "main/reviewer"})
		if err == nil {
			t.Fatal("expected ownership denial error")
		}
	})
}

func intPtr(i int) *int { return &i }

func TestUnwrapHostCallEnvelope(t *testing.T) {
	t.Run("result payload is unwrapped", func(t *testing.T) {
		got, err := unwrapHostCallEnvelope([]byte(`{"result":{"messages":[]}}`))
		if err != nil {
			t.Fatalf("unwrap: %v", err)
		}
		if got != `{"messages":[]}` {
			t.Fatalf("got %q", got)
		}
	})
	t.Run("error field surfaces", func(t *testing.T) {
		_, err := unwrapHostCallEnvelope([]byte(`{"error":"agent not found"}`))
		if err == nil || err.Error() != "agent not found" {
			t.Fatalf("err = %v, want 'agent not found'", err)
		}
	})
	t.Run("empty result with no error is success", func(t *testing.T) {
		got, err := unwrapHostCallEnvelope([]byte(`{}`))
		if err != nil || got != "" {
			t.Fatalf("got (%q, %v), want (\"\", nil)", got, err)
		}
	})
	t.Run("malformed json errors", func(t *testing.T) {
		if _, err := unwrapHostCallEnvelope([]byte(`not-json`)); err == nil {
			t.Fatal("expected error for malformed envelope")
		}
	})
}
