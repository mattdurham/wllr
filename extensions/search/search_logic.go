package main

// search_logic.go — pure search/paging/cache logic, host-independent.
// The wasip1-only main.go wires execFn/getenvFn to the wllr SDK; tests
// substitute fakes. No build tag so `go test` runs on the host.

import (
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"regexp"
	"strings"
	"time"
)

// ─── Tunables ────────────────────────────────────────────────────────────────

const (
	cacheTTL         = 5 * time.Minute
	maxCacheBytes    = 50 << 20 // 50MB total across cached result sets
	streamCapBytes   = 25 << 20 // 25MB awk cap on the rg --json stream
	maxPageSize      = 200
	defaultPageSize  = 50
	maxContextLines  = 5
	maxTextLen       = 300  // per match/context line, keeps pages lean
	maxColumns       = 1000 // rg --max-columns: skip binary-ish/minified lines
	maxCountPerFile  = 2000 // rg --max-count per file
	maxCollectedHits = 100_000
)

// ─── Dependency injection (wired in main.go for wasip1, faked in tests) ──────

var (
	execFn   = func(cmd, dir string) (string, error) { return "", fmt.Errorf("exec not wired") }
	getenvFn = os.Getenv
	nowFn    = time.Now
)

// ─── Types ───────────────────────────────────────────────────────────────────

type matchEntry struct {
	Path   string   `json:"path"`
	Line   int      `json:"line"`
	Text   string   `json:"text"`
	Before []string `json:"before,omitempty"`
	After  []string `json:"after,omitempty"`
}

type cacheEntry struct {
	matches   []matchEntry
	complete  bool   // false when the 25MB stream cap cut the result set
	head      string // git HEAD at search time; "" when not a repo
	created   time.Time
	byteSize  int
	expiresAt time.Time
}

var (
	cache      = map[string]*cacheEntry{}
	cacheBytes int
)

type searchArgs struct {
	Pattern       string `json:"pattern"`
	Path          string `json:"path"`
	Glob          string `json:"glob"`
	Literal       bool   `json:"literal"`
	CaseSensitive *bool  `json:"case_sensitive"` // pointer to distinguish unset
	Context       int    `json:"context"`
	PageSize      int    `json:"page_size"`
	Refresh       bool   `json:"refresh"`
}

type pageArgs struct {
	ResultID string `json:"result_id"`
	Offset   int    `json:"offset"`
	Limit    int    `json:"limit"`
}

// ─── Public entry points (called from main.go's OnToolCall) ──────────────────

func runSearch(input json.RawMessage) (string, bool) {
	var a searchArgs
	if err := json.Unmarshal(input, &a); err != nil {
		return "search: invalid_params: " + err.Error(), true
	}
	if a.Pattern == "" {
		return "search: invalid_params: pattern is required", true
	}
	if a.Context < 0 || a.Context > maxContextLines {
		return fmt.Sprintf("search: invalid_params: context must be 0-%d", maxContextLines), true
	}
	pageSize := a.PageSize
	if pageSize <= 0 {
		pageSize = defaultPageSize
	}
	if pageSize > maxPageSize {
		pageSize = maxPageSize
	}
	caseSensitive := a.CaseSensitive == nil || *a.CaseSensitive

	// Regex pre-validation (Rust regex syntax ≈ Go RE2 for common constructs).
	if !a.Literal {
		if _, err := regexp.Compile(a.Pattern); err != nil {
			return "search: invalid_regex: " + err.Error(), true
		}
	}

	rgPath, err := resolveScopePath(a.Path)
	if err != nil {
		return "search: " + err.Error(), true
	}

	head := gitHead()
	key := cacheKey(a, caseSensitive, head)

	if !a.Refresh {
		if e, ok := cache[key]; ok && nowFn().Before(e.expiresAt) {
			return marshalSearchResult(key, e, pageSize, true, 0, head), false
		}
	}

	start := nowFn()
	matches, complete, err := executeRipgrep(a, rgPath, caseSensitive)
	if err != nil {
		return "search: " + err.Error(), true
	}
	elapsed := time.Since(start).Milliseconds()

	entry := &cacheEntry{
		matches:   matches,
		complete:  complete,
		head:      head,
		created:   nowFn(),
		expiresAt: nowFn().Add(cacheTTL),
	}
	for _, m := range matches {
		entry.byteSize += len(m.Path) + len(m.Text) + 64
		for _, c := range m.Before {
			entry.byteSize += len(c)
		}
		for _, c := range m.After {
			entry.byteSize += len(c)
		}
	}
	putCache(key, entry)
	return marshalSearchResult(key, entry, pageSize, false, elapsed, head), false
}

func runSearchPage(input json.RawMessage) (string, bool) {
	var a pageArgs
	if err := json.Unmarshal(input, &a); err != nil {
		return "search_page: invalid_params: " + err.Error(), true
	}
	if a.ResultID == "" {
		return "search_page: invalid_params: result_id is required", true
	}
	e, ok := cache[a.ResultID]
	if !ok || nowFn().After(e.expiresAt) {
		if ok {
			removeCache(a.ResultID)
		}
		return "search_page: result_not_found: result expired or unknown; re-run search", true
	}
	if a.Offset < 0 {
		return "search_page: invalid_params: offset must be >= 0", true
	}
	if a.Offset > len(e.matches) {
		return fmt.Sprintf(
			"search_page: offset_out_of_range: offset %d exceeds total %d",
			a.Offset,
			len(e.matches),
		), true
	}
	limit := a.Limit
	if limit <= 0 {
		limit = defaultPageSize
	}
	if limit > maxPageSize {
		limit = maxPageSize
	}
	end := a.Offset + limit
	if end > len(e.matches) {
		end = len(e.matches)
	}
	page := e.matches[a.Offset:end]

	stale := e.head != "" && gitHead() != e.head
	remaining := int(e.expiresAt.Sub(nowFn()).Seconds())
	if remaining < 0 {
		remaining = 0
	}
	var nextOffset *int
	if end < len(e.matches) {
		v := end
		nextOffset = &v
	}
	return mustJSON(map[string]any{
		"result_id":    a.ResultID,
		"matches":      page,
		"total":        len(e.matches),
		"returned":     len(page),
		"next_offset":  nextOffset,
		"complete":     e.complete,
		"stale":        stale,
		"expires_in_s": remaining,
	}), false
}

// ─── rg execution ────────────────────────────────────────────────────────────

func executeRipgrep(a searchArgs, rgPath string, caseSensitive bool) ([]matchEntry, bool, error) {
	var b strings.Builder
	b.WriteString("rg --json --no-messages --max-columns ")
	b.WriteString(fmt.Sprint(maxColumns))
	b.WriteString(" --max-count ")
	b.WriteString(fmt.Sprint(maxCountPerFile))
	if !caseSensitive {
		b.WriteString(" -i")
	}
	if a.Literal {
		b.WriteString(" -F")
	}
	if a.Context > 0 {
		fmt.Fprintf(&b, " -C %d", a.Context)
	}
	if a.Glob != "" {
		b.WriteString(" --glob ")
		b.WriteString(shellQuote(a.Glob))
	}
	b.WriteString(" -- ")
	b.WriteString(shellQuote(a.Pattern))
	b.WriteString(" ")
	b.WriteString(shellQuote(rgPath))
	// Stream cap: awk closes the pipe (SIGPIPE to rg) after max bytes.
	fmt.Fprintf(&b, " 2>&1 | awk -v max=%d '{n+=length($0)+1; if(n>max) exit; print}'", streamCapBytes)

	out, err := execFn(b.String(), projectRoot())
	if err != nil {
		low := strings.ToLower(out + err.Error())
		if strings.Contains(low, "command not found") || strings.Contains(low, "no such file or directory: rg") {
			return nil, false, fmt.Errorf("rg_unavailable: ripgrep (rg) is not installed on the host")
		}
		return nil, false, fmt.Errorf("internal: rg failed: %v", err)
	}
	return parseRGJSON(out)
}

// parseRGJSON parses `rg --json` output into match entries, attaching context
// lines to neighbouring matches. complete=false when the stream cap truncated
// output (detected via missing summary line).
func parseRGJSON(out string) ([]matchEntry, bool, error) {
	type rgPath struct {
		Text string `json:"text"`
	}
	type rgLines struct {
		Text string `json:"text"`
	}
	type rgMsg struct {
		Type string `json:"type"`
		Data struct {
			Path       rgPath  `json:"path"`
			Lines      rgLines `json:"lines"`
			LineNumber int     `json:"line_number"`
		} `json:"data"`
	}

	var matches []matchEntry
	var pending []string
	sawSummary := false

	for _, line := range strings.Split(out, "\n") {
		if len(matches) >= maxCollectedHits {
			break
		}
		if len(line) < 10 || line[0] != '{' {
			continue // blank or rg stderr noise
		}
		var m rgMsg
		if err := json.Unmarshal([]byte(line), &m); err != nil {
			continue
		}
		switch m.Type {
		case "match":
			e := matchEntry{
				Path: m.Data.Path.Text,
				Line: m.Data.LineNumber,
				Text: clip(m.Data.Lines.Text),
			}
			if len(pending) > 0 && len(matches) > 0 {
				matches[len(matches)-1].After = pending
				e.Before = pending
			}
			pending = nil
			matches = append(matches, e)
		case "context":
			pending = append(pending, clip(m.Data.Lines.Text))
		case "summary":
			sawSummary = true
		}
	}
	if len(pending) > 0 && len(matches) > 0 {
		matches[len(matches)-1].After = pending
	}
	if matches == nil {
		matches = []matchEntry{}
	}
	return matches, sawSummary, nil
}

// ─── Cache ───────────────────────────────────────────────────────────────────

// cacheKey is a deterministic hash of the query parameters (plus git HEAD), so
// an identical re-query within the TTL hits the same cache entry and result_id.
func cacheKey(a searchArgs, caseSensitive bool, head string) string {
	h, _ := json.Marshal([]any{a.Pattern, a.Path, a.Glob, a.Literal, caseSensitive, a.Context, head})
	sum := sha256.Sum256(h)
	return "sr_" + hex.EncodeToString(sum[:])[:12]
}

func putCache(key string, e *cacheEntry) {
	evict()
	cache[key] = e
	cacheBytes += e.byteSize
	evict()
}

func removeCache(key string) {
	if e, ok := cache[key]; ok {
		cacheBytes -= e.byteSize
		delete(cache, key)
	}
}

func evict() {
	now := nowFn()
	for k, e := range cache {
		if now.After(e.expiresAt) {
			removeCache(k)
		}
	}
	for cacheBytes > maxCacheBytes && len(cache) > 0 {
		var oldestKey string
		var oldest time.Time
		for k, e := range cache {
			if oldestKey == "" || e.created.Before(oldest) {
				oldestKey, oldest = k, e.created
			}
		}
		removeCache(oldestKey)
	}
}

// ─── Helpers ─────────────────────────────────────────────────────────────────

func marshalSearchResult(id string, e *cacheEntry, pageSize int, cached bool, elapsedMs int64, head string) string {
	total := len(e.matches)
	end := pageSize
	if end > total {
		end = total
	}
	var nextOffset *int
	if end < total {
		v := end
		nextOffset = &v
	}
	remaining := int(e.expiresAt.Sub(nowFn()).Seconds())
	if remaining < 0 {
		remaining = 0
	}
	stale := e.head != "" && head != "" && head != e.head
	return mustJSON(map[string]any{
		"result_id":    id,
		"matches":      e.matches[:end],
		"total":        total,
		"returned":     end,
		"next_offset":  nextOffset,
		"complete":     e.complete,
		"cached":       cached,
		"stale":        stale,
		"expires_in_s": remaining,
		"elapsed_ms":   elapsedMs,
	})
}

func projectRoot() string {
	if v := getenvFn("WLLR_CWD"); v != "" {
		return v
	}
	return "."
}

// resolveScopePath confines the search root to the project directory.
func resolveScopePath(p string) (string, error) {
	root := projectRoot()
	if p == "" {
		p = "."
	}
	if filepath.IsAbs(p) {
		clean := filepath.Clean(p)
		rootAbs, rerr := filepath.Abs(root)
		if rerr == nil && (clean == rootAbs || strings.HasPrefix(clean, rootAbs+string(os.PathSeparator))) {
			if _, err := os.Stat(clean); err != nil {
				return "", fmt.Errorf("path_not_found: %s", p)
			}
			return clean, nil
		}
		return "", fmt.Errorf("path_out_of_scope: %s is outside the project root %s", p, root)
	}
	clean := filepath.Clean(p)
	if clean == ".." || strings.HasPrefix(clean, ".."+string(os.PathSeparator)) {
		return "", fmt.Errorf("path_out_of_scope: %s escapes the project root", p)
	}
	abs := filepath.Join(root, clean)
	if _, err := os.Stat(abs); err != nil {
		return "", fmt.Errorf("path_not_found: %s", p)
	}
	return clean, nil
}

func gitHead() string {
	out, err := execFn("git rev-parse HEAD 2>/dev/null", projectRoot())
	if err != nil {
		return ""
	}
	return strings.TrimSpace(out)
}

func shellQuote(s string) string {
	return "'" + strings.ReplaceAll(s, "'", `'\''`) + "'"
}

func clip(s string) string {
	s = strings.TrimSuffix(s, "\n")
	if len(s) > maxTextLen {
		return s[:maxTextLen] + "…"
	}
	return s
}

func mustJSON(v any) string {
	b, err := json.Marshal(v)
	if err != nil {
		return `{"error":"internal: marshal failed"}`
	}
	return string(b)
}
