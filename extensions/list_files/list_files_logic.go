package main

// list_files_logic.go — pure listing/paging/cache logic, host-independent.
// The wasip1-only main.go wires execFn/getenvFn to the wllr SDK; tests
// substitute fakes. No build tag so `go test` runs on the host.

import (
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"sort"
	"strings"
	"time"
)

// ─── Tunables ────────────────────────────────────────────────────────────────

const (
	cacheTTL         = 5 * time.Minute
	maxCacheBytes    = 50 << 20 // 50MB total across cached result sets
	streamCapBytes   = 25 << 20 // 25MB awk cap on the rg --files stream
	maxPageSize      = 1000
	defaultPageSize  = 200
	maxDepthLimit    = 100
	maxCollectedHits = 200_000
	truncSentinel    = "__WLLR_LIST_TRUNC__"
)

// ─── Dependency injection (wired in main.go for wasip1, faked in tests) ──────

var (
	execFn   = func(cmd, dir string) (string, error) { return "", fmt.Errorf("exec not wired") }
	getenvFn = os.Getenv
	nowFn    = time.Now
)

// ─── Types ───────────────────────────────────────────────────────────────────

type cacheEntry struct {
	entries   []string // sorted, relative paths; directories carry a trailing "/"
	complete  bool     // false when the 25MB stream cap cut the listing
	truncated bool     // alias carried for clarity in results
	head      string   // git HEAD at list time; "" when not a repo
	created   time.Time
	byteSize  int
	expiresAt time.Time
}

var (
	cache      = map[string]*cacheEntry{}
	cacheBytes int
)

type listArgs struct {
	Path     string `json:"path"`
	Glob     string `json:"glob"`
	Type     string `json:"type"` // files | dirs | both
	MaxDepth int    `json:"max_depth"`
	Hidden   *bool  `json:"hidden"` // pointer to distinguish unset
	PageSize int    `json:"page_size"`
	Refresh  bool   `json:"refresh"`
}

type pageArgs struct {
	ResultID string `json:"result_id"`
	Offset   int    `json:"offset"`
	Limit    int    `json:"limit"`
}

// ─── Public entry points (called from main.go's OnToolCall) ──────────────────

func runListFiles(input json.RawMessage) (string, bool) {
	var a listArgs
	if err := json.Unmarshal(input, &a); err != nil {
		return "list_files: invalid_params: " + err.Error(), true
	}
	reqType := a.Type
	if reqType == "" {
		reqType = "files"
	}
	switch reqType {
	case "files", "dirs", "both":
	default:
		return "list_files: invalid_params: type must be files, dirs, or both", true
	}
	if a.MaxDepth < 0 || a.MaxDepth > maxDepthLimit {
		return fmt.Sprintf("list_files: invalid_params: max_depth must be 0-%d", maxDepthLimit), true
	}
	pageSize := a.PageSize
	if pageSize <= 0 {
		pageSize = defaultPageSize
	}
	if pageSize > maxPageSize {
		pageSize = maxPageSize
	}
	hidden := a.Hidden != nil && *a.Hidden

	scopePath, err := resolveScopePath(a.Path)
	if err != nil {
		return "list_files: " + err.Error(), true
	}

	head := gitHead()
	key := cacheKey(a, reqType, hidden, head)

	if !a.Refresh {
		if e, ok := cache[key]; ok && nowFn().Before(e.expiresAt) {
			return marshalListResult(key, e, pageSize, true, 0, head), false
		}
	}

	start := nowFn()
	files, complete, err := executeRgFiles(a, scopePath, hidden)
	if err != nil {
		return "list_files: " + err.Error(), true
	}
	elapsed := time.Since(start).Milliseconds()

	entries := buildEntries(files, reqType)

	entry := &cacheEntry{
		entries:   entries,
		complete:  complete,
		truncated: !complete,
		head:      head,
		created:   nowFn(),
		expiresAt: nowFn().Add(cacheTTL),
	}
	for _, p := range entries {
		entry.byteSize += len(p) + 16
	}
	putCache(key, entry)
	return marshalListResult(key, entry, pageSize, false, elapsed, head), false
}

func runListFilesPage(input json.RawMessage) (string, bool) {
	var a pageArgs
	if err := json.Unmarshal(input, &a); err != nil {
		return "list_files_page: invalid_params: " + err.Error(), true
	}
	if a.ResultID == "" {
		return "list_files_page: invalid_params: result_id is required", true
	}
	e, ok := cache[a.ResultID]
	if !ok || nowFn().After(e.expiresAt) {
		if ok {
			removeCache(a.ResultID)
		}
		return "list_files_page: result_not_found: result expired or unknown; re-run list_files", true
	}
	if a.Offset < 0 {
		return "list_files_page: invalid_params: offset must be >= 0", true
	}
	if a.Offset > len(e.entries) {
		return fmt.Sprintf(
			"list_files_page: offset_out_of_range: offset %d exceeds total %d",
			a.Offset,
			len(e.entries),
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
	if end > len(e.entries) {
		end = len(e.entries)
	}
	page := e.entries[a.Offset:end]

	stale := e.head != "" && gitHead() != e.head
	remaining := int(e.expiresAt.Sub(nowFn()).Seconds())
	if remaining < 0 {
		remaining = 0
	}
	var nextOffset *int
	if end < len(e.entries) {
		v := end
		nextOffset = &v
	}
	return mustJSON(map[string]any{
		"result_id":    a.ResultID,
		"files":        page,
		"total":        len(e.entries),
		"returned":     len(page),
		"next_offset":  nextOffset,
		"truncated":    e.truncated,
		"complete":     e.complete,
		"stale":        stale,
		"expires_in_s": remaining,
	}), false
}

// ─── rg execution ────────────────────────────────────────────────────────────

func executeRgFiles(a listArgs, scopePath string, hidden bool) ([]string, bool, error) {
	var b strings.Builder
	b.WriteString("rg --files --no-messages")
	if hidden {
		b.WriteString(" --hidden")
	}
	if a.MaxDepth > 0 {
		fmt.Fprintf(&b, " --max-depth %d", a.MaxDepth)
	}
	if a.Glob != "" {
		b.WriteString(" --glob ")
		b.WriteString(shellQuote(a.Glob))
	}
	b.WriteString(" -- ")
	b.WriteString(shellQuote(scopePath))
	// Stream cap: awk closes the pipe (SIGPIPE to rg) after max bytes. When it
	// cuts output it prints a sentinel so we can report complete=false.
	fmt.Fprintf(&b, " 2>&1 | awk -v max=%d '{n+=length($0)+1; if(n>max){print \"%s\"; exit} print}'",
		streamCapBytes, truncSentinel)

	out, err := execFn(b.String(), projectRoot())
	if err != nil {
		low := strings.ToLower(out + err.Error())
		if strings.Contains(low, "command not found") || strings.Contains(low, "no such file or directory: rg") {
			return nil, false, fmt.Errorf("rg_unavailable: ripgrep (rg) is not installed on the host")
		}
		return nil, false, fmt.Errorf("internal: rg failed: %v", err)
	}
	return parseRgFiles(out)
}

// parseRgFiles splits newline-separated rg --files output, strips the truncation
// sentinel, and reports complete=false when the stream cap fired.
func parseRgFiles(out string) ([]string, bool, error) {
	complete := true
	var files []string
	for _, line := range strings.Split(out, "\n") {
		if line == "" {
			continue
		}
		if line == truncSentinel {
			complete = false
			continue
		}
		if len(files) >= maxCollectedHits {
			complete = false
			break
		}
		files = append(files, normalizePath(line))
	}
	return files, complete, nil
}

// normalizePath strips the leading "./" that rg --files emits when the scope
// path is ".", so entries read as clean project-relative paths.
func normalizePath(p string) string {
	return strings.TrimPrefix(p, "./")
}

// buildEntries turns a raw file list into the requested entry set: files, the
// unique directories containing them, or both, all sorted. Directories carry a
// trailing "/" so they are visually distinct from files.
func buildEntries(files []string, reqType string) []string {
	switch reqType {
	case "dirs":
		return deriveDirs(files)
	case "both":
		dirs := deriveDirs(files)
		merged := make([]string, 0, len(files)+len(dirs))
		merged = append(merged, files...)
		merged = append(merged, dirs...)
		sort.Strings(merged)
		return dedupeSorted(merged)
	default: // files
		sorted := append([]string(nil), files...)
		sort.Strings(sorted)
		return sorted
	}
}

// deriveDirs returns the sorted, unique set of directories that contain the
// given files, each with a trailing "/". Root-level files contribute no entry.
func deriveDirs(files []string) []string {
	set := map[string]struct{}{}
	for _, f := range files {
		dir := filepath.Dir(f)
		for dir != "." && dir != "/" && dir != "" {
			set[dir+"/"] = struct{}{}
			dir = filepath.Dir(dir)
		}
	}
	dirs := make([]string, 0, len(set))
	for d := range set {
		dirs = append(dirs, d)
	}
	sort.Strings(dirs)
	return dirs
}

func dedupeSorted(s []string) []string {
	if len(s) == 0 {
		return s
	}
	out := s[:1]
	for _, v := range s[1:] {
		if v != out[len(out)-1] {
			out = append(out, v)
		}
	}
	return out
}

// ─── Cache ───────────────────────────────────────────────────────────────────

// cacheKey is a deterministic hash of the query parameters (plus git HEAD), so
// an identical re-query within the TTL hits the same cache entry and result_id.
func cacheKey(a listArgs, reqType string, hidden bool, head string) string {
	h, _ := json.Marshal([]any{a.Path, a.Glob, reqType, a.MaxDepth, hidden, head})
	sum := sha256.Sum256(h)
	return "lf_" + hex.EncodeToString(sum[:])[:12]
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

func marshalListResult(id string, e *cacheEntry, pageSize int, cached bool, elapsedMs int64, head string) string {
	total := len(e.entries)
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
		"files":        e.entries[:end],
		"total":        total,
		"returned":     end,
		"next_offset":  nextOffset,
		"truncated":    e.truncated,
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

// resolveScopePath confines the listing root to the project directory.
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

func mustJSON(v any) string {
	b, err := json.Marshal(v)
	if err != nil {
		return `{"error":"internal: marshal failed"}`
	}
	return string(b)
}
