package main

import "testing"

func TestDuckDuckGoHTMLURL(t *testing.T) {
	want := "https://html.duckduckgo.com/html/?q=queue+bug%3F"
	if got := duckDuckGoHTMLURL("queue bug?"); got != want {
		t.Fatalf("duckDuckGoHTMLURL() = %q, want %q", got, want)
	}
}

func TestParseDuckDuckGoHTML(t *testing.T) {
	body := `<div class="result results_links web-result"><div class="links_main"><a rel="nofollow" class="result__a" href="//duckduckgo.com/l/?uddg=https%3A%2F%2Fexample.com%2Fdocs&amp;rut=x">Example <b>Docs</b></a><a class="result__snippet">Useful &amp; relevant <b>documentation</b>.</a></div></div>`
	got := parseDuckDuckGoHTML(body)
	if len(got) != 1 {
		t.Fatalf("got %d results, want 1", len(got))
	}
	if got[0].Title != "Example Docs" || got[0].Link != "https://example.com/docs" || got[0].Snippet != "Useful & relevant documentation." {
		t.Fatalf("unexpected result: %#v", got[0])
	}
}

func TestParseDuckDuckGoHTMLEmptyOrMalformed(t *testing.T) {
	if got := parseDuckDuckGoHTML("<html>no results</html>"); len(got) != 0 {
		t.Fatalf("got %d results from empty page", len(got))
	}
	if got := parseDuckDuckGoHTML(`<div class="result"><a class="result__a">missing href</a></div>`); len(got) != 0 {
		t.Fatalf("got %d results from malformed result", len(got))
	}
}
