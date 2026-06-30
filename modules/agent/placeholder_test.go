package agent

import "testing"

func TestPlaceholderForEmptyResponse(t *testing.T) {
	cases := []struct {
		name      string
		collected string
		cancelled bool
		want      string
	}{
		{"non-empty passes through", "hello", false, "hello"},
		{"non-empty passes through even when cancelled", "partial", true, "partial"},
		{"empty + not cancelled = tool-only", "", false, placeholderToolOnly},
		{"empty + cancelled = cancelled", "", true, placeholderCancelled},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			if got := placeholderForEmptyResponse(c.collected, c.cancelled); got != c.want {
				t.Errorf("placeholderForEmptyResponse(%q, %v) = %q, want %q", c.collected, c.cancelled, got, c.want)
			}
		})
	}
}
