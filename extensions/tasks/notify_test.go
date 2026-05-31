//go:build !wasip1

package main

import "testing"

func TestShouldNotify(t *testing.T) {
	cases := []struct {
		old  string
		new  string
		want bool
	}{
		{"pending", "completed", true},
		{"in_progress", "completed", true},
		{"pending", "blocked", true},
		{"in_progress", "blocked", true},
		{"pending", "in_progress", false},
		{"completed", "completed", false},
		{"blocked", "blocked", false},
		{"blocked", "completed", true},
		{"", "completed", true},
	}
	for _, c := range cases {
		got := shouldNotify(c.old, c.new)
		if got != c.want {
			t.Errorf("shouldNotify(%q, %q) = %v, want %v", c.old, c.new, got, c.want)
		}
	}
}
