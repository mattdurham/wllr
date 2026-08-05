package main

import (
	"html"
	"net/url"
	"regexp"
	"strings"
)

type SearchResult struct {
	Title   string `json:"title"`
	Link    string `json:"link"`
	Snippet string `json:"snippet"`
}

var (
	ddgTitleRE   = regexp.MustCompile(`(?s)<a[^>]*class=["'][^"']*result__a[^"']*["'][^>]*href=["']([^"']+)["'][^>]*>(.*?)</a>`)
	ddgSnippetRE = regexp.MustCompile(`(?s)<a[^>]*class=["'][^"']*result__snippet[^"']*["'][^>]*>(.*?)</a>`)
	tagRE        = regexp.MustCompile(`(?s)<[^>]+>`)
)

func duckDuckGoHTMLURL(query string) string {
	return "https://html.duckduckgo.com/html/?q=" + url.QueryEscape(query)
}

func parseDuckDuckGoHTML(body string) []SearchResult {
	var results []SearchResult
	titles := ddgTitleRE.FindAllStringSubmatchIndex(body, 10)
	for i, title := range titles {
		link := decodeDuckDuckGoLink(body[title[2]:title[3]])
		if link == "" {
			continue
		}
		end := len(body)
		if i+1 < len(titles) {
			end = titles[i+1][0]
		}
		segment := body[title[1]:end]
		snippet := ddgSnippetRE.FindStringSubmatch(segment)
		result := SearchResult{
			Title: cleanHTMLText(body[title[4]:title[5]]),
			Link:  link,
		}
		if len(snippet) == 2 {
			result.Snippet = cleanHTMLText(snippet[1])
		}
		results = append(results, result)
	}
	return results
}

func cleanHTMLText(value string) string {
	text := strings.Join(strings.Fields(html.UnescapeString(tagRE.ReplaceAllString(value, " "))), " ")
	return strings.ReplaceAll(text, " .", ".")
}

func decodeDuckDuckGoLink(raw string) string {
	raw = html.UnescapeString(raw)
	if strings.HasPrefix(raw, "//") {
		raw = "https:" + raw
	}
	if parsed, err := url.Parse(raw); err == nil {
		if target := parsed.Query().Get("uddg"); target != "" {
			return target
		}
		if strings.HasPrefix(parsed.Scheme, "http") {
			return parsed.String()
		}
	}
	return raw
}
