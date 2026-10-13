package main

import "testing"

func TestShouldRecordUserPrompt(t *testing.T) {
	tests := []struct {
		name    string
		agentID string
		queued  bool
		steer   bool
		want    bool
	}{
		{name: "root prompt with empty ID", want: true},
		{name: "root prompt", agentID: "main", want: true},
		{name: "queued root work", agentID: "main", queued: true},
		{name: "queued root steer", agentID: "main", queued: true, steer: true, want: true},
		{name: "sub-agent prompt", agentID: "main/worker"},
		{name: "sub-agent steer", agentID: "main/worker", queued: true, steer: true},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if got := shouldRecordUserPrompt(tt.agentID, tt.queued, tt.steer); got != tt.want {
				t.Errorf("shouldRecordUserPrompt(%q, %t, %t) = %t, want %t", tt.agentID, tt.queued, tt.steer, got, tt.want)
			}
		})
	}
}
