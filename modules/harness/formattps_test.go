package harness

import "testing"

// TestFormatTpsLive verifies the composed tps statusline segment: the bars
// ride the same status value as the number, and either half missing hides
// the composition (a short turn shows just the number; no number hides both).
func TestFormatTpsLive(t *testing.T) {
	cases := []struct {
		name  string
		tps   float64
		spark string
		want  string
	}{
		{"no rate", 0, "▃▅▇", ""},
		{"number only", 87, "", "87 t/s"},
		{"composed", 87, "▃▅▇", "87 t/s ▃▅▇"},
		{"sub-one rate", 0.5, "▁", "<1 t/s ▁"},
	}
	for _, tc := range cases {
		if got := formatTpsLive(tc.tps, tc.spark); got != tc.want {
			t.Errorf("%s: formatTpsLive(%v, %q) = %q, want %q", tc.name, tc.tps, tc.spark, got, tc.want)
		}
	}
}
