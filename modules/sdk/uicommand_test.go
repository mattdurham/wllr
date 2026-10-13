package sdk

// NOTE: Any changes to this file must be reflected in the corresponding SPECS.md or NOTES.md.

import "testing"

func TestSlashWordAt(t *testing.T) {
	cases := []struct {
		in   string
		want int
	}{
		{"/he", 0},
		{"/", 0},
		{"hi /he", 3},
		{"hi there /mo", 9},
		{"/help me", -1},
		{"http://x", -1},
		{"nothing", -1},
		{"", -1},
		{"a /b/c", -1}, // last slash is mid-word
		{"x /y", 2},
	}
	for _, c := range cases {
		if got := SlashWordAt(c.in); got != c.want {
			t.Errorf("SlashWordAt(%q) = %d, want %d", c.in, got, c.want)
		}
	}
}

func TestCommandQuery(t *testing.T) {
	cases := []struct {
		in    string
		query string
		ok    bool
	}{
		{"/Hel", "hel", true},
		{"hi /Mo", "mo", true},
		{"/help me", "", false},
		{"http://x", "", false},
		{"nothing", "", false},
	}
	for _, c := range cases {
		q, ok := CommandQuery(c.in)
		if q != c.query || ok != c.ok {
			t.Errorf("CommandQuery(%q) = (%q,%v), want (%q,%v)", c.in, q, ok, c.query, c.ok)
		}
	}
}

func TestCommandListerFunc(t *testing.T) {
	var l CommandLister = CommandListerFunc(func() []CommandInfo {
		return []CommandInfo{{Name: "model", Instant: true}}
	})
	got := l.ListCommands()
	if len(got) != 1 || got[0].Name != "model" || !got[0].Instant {
		t.Fatalf("unexpected list: %+v", got)
	}
}
