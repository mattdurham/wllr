package main

import (
	"encoding/json"
	"fmt"
	"os"
	"strings"
	"testing"
	"time"
)

// fakeExec returns canned rg --json output and records commands.
type fakeExecT struct {
	out  string
	err  error
	cmds []string
}

func (f *fakeExecT) run(cmd, dir string) (string, error) {
	f.cmds = append(f.cmds, cmd)
	return f.out, f.err
}

var testRoot string

func setup(f *fakeExecT) {
	execFn = f.run
	if testRoot == "" {
		dir, err := os.MkdirTemp("", "searchtest")
		if err != nil {
			panic(err)
		}
		testRoot = dir
	}
	getenvFn = func(k string) string {
		if k == "WLLR_CWD" {
			return testRoot
		}
		return ""
	}
	cache = map[string]*cacheEntry{}
	cacheBytes = 0
}

func rgLine(typ, path, text string, line int) string {
	return fmt.Sprintf(`{"type":%q,"data":{"path":{"text":%q},"lines":{"text":%q},"line_number":%d}}`,
		typ, path, text, line)
}

func TestParseMatches(t *testing.T) {
	f := &fakeExecT{out: strings.Join([]string{
		rgLine("begin", "a.go", "", 0),
		rgLine("match", "a.go", "func foo() {", 10),
		rgLine("context", "a.go", "\treturn 1", 11),
		rgLine("match", "a.go", "func bar() {", 20),
		rgLine("end", "a.go", "", 0),
		`{"type":"summary","data":{"stats":{}}}`,
	}, "\n")}
	setup(f)

	res, isErr := runSearch(json.RawMessage(`{"pattern":"func","context":1}`))
	if isErr {
		t.Fatalf("unexpected error: %s", res)
	}
	var r struct {
		ResultID   string       `json:"result_id"`
		Matches    []matchEntry `json:"matches"`
		Total      int          `json:"total"`
		Complete   bool         `json:"complete"`
		Cached     bool         `json:"cached"`
		NextOffset *int         `json:"next_offset"`
	}
	if err := json.Unmarshal([]byte(res), &r); err != nil {
		t.Fatal(err)
	}
	if r.Total != 2 || len(r.Matches) != 2 {
		t.Fatalf("want 2 matches, got %d", r.Total)
	}
	if !r.Complete {
		t.Fatal("expected complete=true (summary present)")
	}
	if r.Cached {
		t.Fatal("first run should not be cached")
	}
	if r.NextOffset != nil {
		t.Fatal("all results fit in page 1; next_offset should be null")
	}
	// Context line attaches between the two matches: after m1, before m2.
	if len(r.Matches[0].After) != 1 || r.Matches[0].After[0] != "\treturn 1" {
		t.Fatalf("context after m1 wrong: %+v", r.Matches[0])
	}
	if len(r.Matches[1].Before) != 1 {
		t.Fatalf("context before m2 wrong: %+v", r.Matches[1])
	}
	if r.ResultID == "" {
		t.Fatal("missing result_id")
	}
}

func TestCacheHitDeterministic(t *testing.T) {
	f := &fakeExecT{out: rgLine("match", "a.go", "x", 1) + "\n" + `{"type":"summary"}`}
	setup(f)
	r1, _ := runSearch(json.RawMessage(`{"pattern":"x"}`))
	r2, _ := runSearch(json.RawMessage(`{"pattern":"x"}`))
	var v1, v2 struct {
		ResultID string `json:"result_id"`
		Cached   bool   `json:"cached"`
	}
	_ = json.Unmarshal([]byte(r1), &v1)
	_ = json.Unmarshal([]byte(r2), &v2)
	if v1.ResultID != v2.ResultID {
		t.Fatalf("identical query must map to same result_id: %s vs %s", v1.ResultID, v2.ResultID)
	}
	if !v2.Cached {
		t.Fatal("second identical query should be cached:true")
	}
	// rg ran once for the search + gitHead calls; count rg invocations only.
	rgRuns := 0
	for _, c := range f.cmds {
		if strings.HasPrefix(c, "rg ") {
			rgRuns++
		}
	}
	if rgRuns != 1 {
		t.Fatalf("rg should have run once, ran %d times", rgRuns)
	}
}

func TestPaging(t *testing.T) {
	var lines []string
	for i := 1; i <= 120; i++ {
		lines = append(lines, rgLine("match", "a.go", fmt.Sprintf("line %d", i), i))
	}
	lines = append(lines, `{"type":"summary"}`)
	f := &fakeExecT{out: strings.Join(lines, "\n")}
	setup(f)

	res, _ := runSearch(json.RawMessage(`{"pattern":"line","page_size":50}`))
	var r struct {
		ResultID   string `json:"result_id"`
		Total      int    `json:"total"`
		Returned   int    `json:"returned"`
		NextOffset *int   `json:"next_offset"`
	}
	_ = json.Unmarshal([]byte(res), &r)
	if r.Total != 120 || r.Returned != 50 || r.NextOffset == nil || *r.NextOffset != 50 {
		t.Fatalf("page 1 wrong: %+v", r)
	}

	// Page 2 via search_page — no new rg run.
	p2, isErr := runSearchPage(json.RawMessage(fmt.Sprintf(`{"result_id":%q,"offset":50,"limit":50}`, r.ResultID)))
	if isErr {
		t.Fatalf("page 2 error: %s", p2)
	}
	var v2 struct {
		Returned   int  `json:"returned"`
		NextOffset *int `json:"next_offset"`
	}
	_ = json.Unmarshal([]byte(p2), &v2)
	if v2.Returned != 50 || v2.NextOffset == nil || *v2.NextOffset != 100 {
		t.Fatalf("page 2 wrong: %+v", v2)
	}

	// Final partial page.
	p3, _ := runSearchPage(json.RawMessage(fmt.Sprintf(`{"result_id":%q,"offset":100,"limit":50}`, r.ResultID)))
	var v3 struct {
		Returned   int  `json:"returned"`
		NextOffset *int `json:"next_offset"`
	}
	_ = json.Unmarshal([]byte(p3), &v3)
	if v3.Returned != 20 || v3.NextOffset != nil {
		t.Fatalf("final page wrong: %+v", v3)
	}

	rgRuns := 0
	for _, c := range f.cmds {
		if strings.HasPrefix(c, "rg ") {
			rgRuns++
		}
	}
	if rgRuns != 1 {
		t.Fatalf("paging must not re-run rg; ran %d times", rgRuns)
	}
}

func TestExpiredResult(t *testing.T) {
	f := &fakeExecT{out: rgLine("match", "a.go", "x", 1) + "\n" + `{"type":"summary"}`}
	setup(f)
	res, _ := runSearch(json.RawMessage(`{"pattern":"x"}`))
	var r struct {
		ResultID string `json:"result_id"`
	}
	_ = json.Unmarshal([]byte(res), &r)

	// Advance time beyond TTL.
	base := time.Now()
	nowFn = func() time.Time { return base.Add(6 * time.Minute) }
	defer func() { nowFn = time.Now }()

	_, isErr := runSearchPage(json.RawMessage(fmt.Sprintf(`{"result_id":%q}`, r.ResultID)))
	if !isErr {
		t.Fatal("expected result_not_found error after TTL")
	}
}

func TestOffsetOutOfRange(t *testing.T) {
	f := &fakeExecT{out: rgLine("match", "a.go", "x", 1) + "\n" + `{"type":"summary"}`}
	setup(f)
	res, _ := runSearch(json.RawMessage(`{"pattern":"x"}`))
	var r struct {
		ResultID string `json:"result_id"`
	}
	_ = json.Unmarshal([]byte(res), &r)
	out, isErr := runSearchPage(json.RawMessage(fmt.Sprintf(`{"result_id":%q,"offset":99}`, r.ResultID)))
	if !isErr || !strings.Contains(out, "offset_out_of_range") {
		t.Fatalf("expected offset_out_of_range, got %s", out)
	}
}

func TestInvalidRegex(t *testing.T) {
	setup(&fakeExecT{})
	out, isErr := runSearch(json.RawMessage(`{"pattern":"[unclosed"}`))
	if !isErr || !strings.Contains(out, "invalid_regex") {
		t.Fatalf("expected invalid_regex, got %s", out)
	}
}

func TestScopeGuard(t *testing.T) {
	setup(&fakeExecT{})
	for _, p := range []string{"..", "../x", "/etc/passwd"} {
		out, isErr := runSearch(json.RawMessage(fmt.Sprintf(`{"pattern":"x","path":%q}`, p)))
		if !isErr || !strings.Contains(out, "path_out_of_scope") {
			t.Fatalf("path %q should be rejected: %s", p, out)
		}
	}
}

func TestPathNotFound(t *testing.T) {
	setup(&fakeExecT{})
	out, isErr := runSearch(json.RawMessage(`{"pattern":"x","path":"definitely-not-here-xyz"}`))
	if !isErr || !strings.Contains(out, "path_not_found") {
		t.Fatalf("expected path_not_found, got %s", out)
	}
}

func TestTruncatedDetection(t *testing.T) {
	// No summary line => complete=false (stream cap cut output).
	f := &fakeExecT{out: rgLine("match", "a.go", "x", 1)}
	setup(f)
	res, _ := runSearch(json.RawMessage(`{"pattern":"x"}`))
	var r struct {
		Complete bool `json:"complete"`
	}
	_ = json.Unmarshal([]byte(res), &r)
	if r.Complete {
		t.Fatal("missing summary should mark complete=false")
	}
}

func TestCommandConstruction(t *testing.T) {
	f := &fakeExecT{out: `{"type":"summary"}`}
	setup(f)
	_, _ = runSearch(
		json.RawMessage(
			`{"pattern":"it's a \"test\"","glob":"*.go","literal":true,"case_sensitive":false,"context":2,"path":"."}`,
		),
	)
	var rgCmd string
	for _, c := range f.cmds {
		if strings.HasPrefix(c, "rg ") {
			rgCmd = c
		}
	}
	for _, want := range []string{"-F", "-i", "-C 2", "--glob '*.go'", "-- 'it'\\''s a \"test\"'", "--max-columns", "awk"} {
		if !strings.Contains(rgCmd, want) {
			t.Fatalf("command missing %q:\n%s", want, rgCmd)
		}
	}
}
