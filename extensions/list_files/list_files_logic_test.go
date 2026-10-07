package main

import (
	"encoding/json"
	"fmt"
	"os"
	"strings"
	"testing"
	"time"
)

// fakeExecT returns canned rg --files output and records commands.
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
	nowFn = time.Now
	if testRoot == "" {
		dir, err := os.MkdirTemp("", "listfilestest")
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

type listResult struct {
	ResultID   string   `json:"result_id"`
	Files      []string `json:"files"`
	Total      int      `json:"total"`
	Returned   int      `json:"returned"`
	NextOffset *int     `json:"next_offset"`
	Truncated  bool     `json:"truncated"`
	Complete   bool     `json:"complete"`
	Cached     bool     `json:"cached"`
}

func list(t *testing.T, input string) listResult {
	t.Helper()
	res, isErr := runListFiles(json.RawMessage(input))
	if isErr {
		t.Fatalf("unexpected error: %s", res)
	}
	var r listResult
	if err := json.Unmarshal([]byte(res), &r); err != nil {
		t.Fatal(err)
	}
	return r
}

func TestParseFiles(t *testing.T) {
	f := &fakeExecT{out: "main.go\npkg/util.go\nREADME.md\n"}
	setup(f)

	r := list(t, `{}`)
	if r.Total != 3 || len(r.Files) != 3 {
		t.Fatalf("want 3 files, got %d", r.Total)
	}
	if !r.Complete || r.Truncated {
		t.Fatalf("expected complete listing, got complete=%v truncated=%v", r.Complete, r.Truncated)
	}
	if r.Files[0] != "README.md" || r.Files[1] != "main.go" || r.Files[2] != "pkg/util.go" {
		t.Fatalf("expected sorted files, got %v", r.Files)
	}
	if r.ResultID == "" {
		t.Fatal("missing result_id")
	}
	if r.Cached {
		t.Fatal("first run should not be cached")
	}
}

func TestTruncationSentinel(t *testing.T) {
	f := &fakeExecT{out: "a.go\n" + truncSentinel + "\n"}
	setup(f)

	r := list(t, `{}`)
	if r.Complete {
		t.Fatal("sentinel should mark complete=false")
	}
	if !r.Truncated {
		t.Fatal("sentinel should mark truncated=true")
	}
	if r.Total != 1 {
		t.Fatalf("sentinel must not count as an entry; total=%d", r.Total)
	}
}

func TestPaging(t *testing.T) {
	var lines []string
	for i := 1; i <= 120; i++ {
		lines = append(lines, fmt.Sprintf("file%03d.go", i))
	}
	f := &fakeExecT{out: strings.Join(lines, "\n") + "\n"}
	setup(f)

	r := list(t, `{"page_size":50}`)
	if r.Total != 120 || r.Returned != 50 || r.NextOffset == nil || *r.NextOffset != 50 {
		t.Fatalf("page 1 wrong: %+v", r)
	}

	p2, isErr := runListFilesPage(
		json.RawMessage(fmt.Sprintf(`{"result_id":%q,"offset":50,"limit":50}`, r.ResultID)),
	)
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

	p3, _ := runListFilesPage(
		json.RawMessage(fmt.Sprintf(`{"result_id":%q,"offset":100,"limit":50}`, r.ResultID)),
	)
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

func TestCacheHitDeterministic(t *testing.T) {
	f := &fakeExecT{out: "a.go\nb.go\n"}
	setup(f)

	r1, _ := runListFiles(json.RawMessage(`{"glob":"*.go"}`))
	r2, _ := runListFiles(json.RawMessage(`{"glob":"*.go"}`))
	var v1, v2 listResult
	_ = json.Unmarshal([]byte(r1), &v1)
	_ = json.Unmarshal([]byte(r2), &v2)
	if v1.ResultID != v2.ResultID {
		t.Fatalf("identical query must map to same result_id: %s vs %s", v1.ResultID, v2.ResultID)
	}
	if !v2.Cached {
		t.Fatal("second identical query should be cached:true")
	}
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

func TestExpiredResult(t *testing.T) {
	f := &fakeExecT{out: "a.go\n"}
	setup(f)
	r := list(t, `{}`)

	base := time.Now()
	nowFn = func() time.Time { return base.Add(6 * time.Minute) }
	defer func() { nowFn = time.Now }()

	_, isErr := runListFilesPage(json.RawMessage(fmt.Sprintf(`{"result_id":%q}`, r.ResultID)))
	if !isErr {
		t.Fatal("expected result_not_found error after TTL")
	}
}

func TestOffsetOutOfRange(t *testing.T) {
	f := &fakeExecT{out: "a.go\n"}
	setup(f)
	r := list(t, `{}`)
	out, isErr := runListFilesPage(
		json.RawMessage(fmt.Sprintf(`{"result_id":%q,"offset":99}`, r.ResultID)),
	)
	if !isErr || !strings.Contains(out, "offset_out_of_range") {
		t.Fatalf("expected offset_out_of_range, got %s", out)
	}
}

func TestDirsDerivation(t *testing.T) {
	f := &fakeExecT{out: "a/b/c.go\na/d.go\ntop.go\n"}
	setup(f)

	r := list(t, `{"type":"dirs"}`)
	want := []string{"a/", "a/b/"}
	if r.Total != len(want) {
		t.Fatalf("want %d dirs, got %d: %v", len(want), r.Total, r.Files)
	}
	for i, w := range want {
		if r.Files[i] != w {
			t.Fatalf("dir %d: want %q got %q (%v)", i, w, r.Files[i], r.Files)
		}
	}
}

func TestBothFilesAndDirs(t *testing.T) {
	f := &fakeExecT{out: "a/b/c.go\ntop.go\n"}
	setup(f)

	r := list(t, `{"type":"both"}`)
	// a/ , a/b/ , a/b/c.go , top.go  (sorted)
	want := []string{"a/", "a/b/", "a/b/c.go", "top.go"}
	if r.Total != len(want) {
		t.Fatalf("want %d entries, got %v", len(want), r.Files)
	}
	for i, w := range want {
		if r.Files[i] != w {
			t.Fatalf("entry %d: want %q got %q (%v)", i, w, r.Files[i], r.Files)
		}
	}
}

func TestInvalidType(t *testing.T) {
	setup(&fakeExecT{})
	out, isErr := runListFiles(json.RawMessage(`{"type":"bogus"}`))
	if !isErr || !strings.Contains(out, "invalid_params") {
		t.Fatalf("expected invalid_params, got %s", out)
	}
}

func TestMaxDepthGuard(t *testing.T) {
	setup(&fakeExecT{})
	out, isErr := runListFiles(json.RawMessage(`{"max_depth":999}`))
	if !isErr || !strings.Contains(out, "invalid_params") {
		t.Fatalf("expected max_depth guard, got %s", out)
	}
}

func TestScopeGuard(t *testing.T) {
	setup(&fakeExecT{})
	for _, p := range []string{"..", "../x", "/etc/passwd"} {
		out, isErr := runListFiles(json.RawMessage(fmt.Sprintf(`{"path":%q}`, p)))
		if !isErr || !strings.Contains(out, "path_out_of_scope") {
			t.Fatalf("path %q should be rejected: %s", p, out)
		}
	}
}

func TestPathNotFound(t *testing.T) {
	setup(&fakeExecT{})
	out, isErr := runListFiles(json.RawMessage(`{"path":"definitely-not-here-xyz"}`))
	if !isErr || !strings.Contains(out, "path_not_found") {
		t.Fatalf("expected path_not_found, got %s", out)
	}
}

func TestCommandConstruction(t *testing.T) {
	f := &fakeExecT{out: ""}
	setup(f)
	_, _ = runListFiles(
		json.RawMessage(`{"glob":"*.go","hidden":true,"max_depth":3,"path":"."}`),
	)
	var cmd string
	for _, c := range f.cmds {
		if strings.HasPrefix(c, "rg ") {
			cmd = c
		}
	}
	for _, want := range []string{"rg --files", "--hidden", "--max-depth 3", "--glob '*.go'", "-- '.'", "awk"} {
		if !strings.Contains(cmd, want) {
			t.Fatalf("command missing %q:\n%s", want, cmd)
		}
	}
}

func TestEmptyListing(t *testing.T) {
	f := &fakeExecT{out: ""}
	setup(f)
	r := list(t, `{}`)
	if r.Total != 0 || len(r.Files) != 0 {
		t.Fatalf("want empty listing, got %v", r.Files)
	}
	if !r.Complete {
		t.Fatal("empty listing should be complete")
	}
}

func TestDotSlashStripped(t *testing.T) {
	// rg --files . emits "./x" paths; they must be normalized to "x".
	f := &fakeExecT{out: "./main.go\n./pkg/a.go\n"}
	setup(f)
	r := list(t, `{}`)
	if len(r.Files) != 2 || r.Files[0] != "main.go" || r.Files[1] != "pkg/a.go" {
		t.Fatalf("expected ./ stripped, got %v", r.Files)
	}
}
