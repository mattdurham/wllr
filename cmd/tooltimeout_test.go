package main

// tooltimeout_test.go — config parsing for the tool-call watchdog duration.

import (
	"testing"
	"time"
)

func TestParseToolCallTimeout(t *testing.T) {
	cases := []struct {
		name    string
		in      any
		want    time.Duration
		wantErr bool
	}{
		{"duration seconds", "90s", 90 * time.Second, false},
		{"duration minutes", "5m", 5 * time.Minute, false},
		{"duration hours", "1h", time.Hour, false},
		{"bare int is millis", 30000, 30 * time.Second, false},
		{"bare int string is millis", "30000", 30 * time.Second, false},
		{"zero resets to default", 0, 0, false},
		{"negative resets", "-5m", -5 * time.Minute, false},
		{"garbage string", "banana", 0, true},
		{"unsupported type", []any{1}, 0, true},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			got, err := parseToolCallTimeout(tc.in)
			if tc.wantErr {
				if err == nil {
					t.Fatalf("parseToolCallTimeout(%v): expected error, got %v", tc.in, got)
				}
				return
			}
			if err != nil {
				t.Fatalf("parseToolCallTimeout(%v): %v", tc.in, err)
			}
			if got != tc.want {
				t.Fatalf("parseToolCallTimeout(%v) = %v, want %v", tc.in, got, tc.want)
			}
		})
	}
}
