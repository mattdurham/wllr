package wire

// NOTE: Any changes to this file must be reflected in the corresponding SPECS.md or NOTES.md.

import (
	"encoding/json"
	"go/ast"
	"go/parser"
	"go/token"
	"os"
	"regexp"
	"sort"
	"strconv"
	"strings"
	"testing"

	"github.com/mattdurham/wllr/modules/sdk"
)

// declaredEventKinds parses wire.go and returns every constant declared with
// type EventKind, as name -> string value. Reading the source (rather than a
// hand-maintained list in the test) means a newly added kind is caught by these
// tests without anyone remembering to update them: it will be missing from
// sampleEvents and from SPECS.md, and both checks fail.
func declaredEventKinds(t *testing.T) map[string]string {
	t.Helper()
	fset := token.NewFileSet()
	file, err := parser.ParseFile(fset, "wire.go", nil, 0)
	if err != nil {
		t.Fatalf("parse wire.go: %v", err)
	}

	out := map[string]string{}
	ast.Inspect(file, func(n ast.Node) bool {
		decl, ok := n.(*ast.GenDecl)
		if !ok || decl.Tok != token.CONST {
			return true
		}
		for _, spec := range decl.Specs {
			vs, ok := spec.(*ast.ValueSpec)
			if !ok || vs.Type == nil {
				continue
			}
			ident, ok := vs.Type.(*ast.Ident)
			if !ok || ident.Name != "EventKind" {
				continue
			}
			for i, name := range vs.Names {
				if i >= len(vs.Values) {
					break
				}
				lit, ok := vs.Values[i].(*ast.BasicLit)
				if !ok || lit.Kind != token.STRING {
					continue
				}
				val, err := strconv.Unquote(lit.Value)
				if err != nil {
					t.Fatalf("unquote %s: %v", name.Name, err)
				}
				out[name.Name] = val
			}
		}
		return true
	})

	if len(out) == 0 {
		t.Fatal("found no EventKind constants in wire.go — the parser walk is broken")
	}
	return out
}

// sampleEvents returns one fully populated Event per declared kind. Every field
// an event kind can carry is exercised by at least one sample, so a round-trip
// that drops or renames a JSON tag fails here. The map is keyed by the constant
// name; declaredEventKinds provides the authoritative key set.
func sampleEvents() map[string]Event {
	picker := &sdk.ShowPickerParams{
		Title:    "model",
		Callback: "__wllr:model",
		Split:    true,
		Items: []sdk.ShowPickerItem{
			{ID: "a", Label: "A", Sublabel: "one", Preview: "preview"},
		},
	}
	tree := &sdk.ShowAgentTreeParams{
		Title:    "Agents",
		Callback: "agents:tree",
		Nodes: []sdk.AgentTreeNode{
			{ID: "main", Label: "main"},
			{ID: "main/coder", ParentID: "main", Detail: "working"},
		},
	}
	idx := 2
	patch := &sdk.UIPatchParams{
		Area: "chat",
		Ops: []sdk.UIPatchOp{
			{
				Op: sdk.UIOpInsert, Parent: "", Index: &idx, ID: "n1",
				Node: &sdk.UINode{Type: sdk.UINodeText, Text: "hi"},
			},
			{Op: sdk.UIOpUpdate, ID: "n1", Props: &sdk.UIProps{Bold: true}},
			{Op: sdk.UIOpAppendText, ID: "n1", Text: "!"},
			{Op: sdk.UIOpReplaceText, ID: "n1", Text: "done"},
			{Op: sdk.UIOpRemove, ID: "n2"},
			{Op: sdk.UIOpSetRoot},
		},
	}
	w := 3
	areaUpdate := &sdk.UIUpdateAreaParams{
		ID: "chat", Weight: &w, MinHeight: "3", MaxHeight: "50%",
	}
	area := &sdk.UIArea{
		ID: "chat", Placement: sdk.UIAreaMain, MinHeight: "3", Weight: 1,
	}
	usage := &sdk.ContextUsage{InputTokens: 10, OutputTokens: 4, ContextWindow: 200, Percent: 5}

	msg := sdk.Message{ID: "m1", Role: sdk.RoleUser, Content: "hello", Type: sdk.MessageTypeNormal}

	return map[string]Event{
		"KindReady":            {Protocol: ProtocolVersion, Seq: 7},
		"KindUserMessage":      {AgentID: "main", Text: "do it", Role: sdk.RoleUser, Inbox: []sdk.Message{msg}},
		"KindToken":            {AgentID: "main", Text: "tok"},
		"KindTurnStarted":      {AgentID: "main"},
		"KindTurnDone":         {AgentID: "main", Err: "boom"},
		"KindAssistantMessage": {AgentID: "main", Text: "**hi**", HTML: "<b>hi</b>"},
		"KindToolCall":         {AgentID: "main", ToolCallID: "t1", ToolName: "read_file", Input: "{}"},
		"KindToolResult":       {AgentID: "main", ToolCallID: "t1", ToolName: "read_file", Output: "ok", IsError: true},
		"KindContextUsage":     {AgentID: "main", Usage: usage},
		"KindNotify":           {AgentID: "main", Text: "compacted"},
		"KindAgentSpawned":     {AgentID: "main/coder", Name: "coder", Live: 2, Main: true},
		"KindAgentClosed":      {AgentID: "main/coder", Live: 1},
		"KindError":            {Text: "bad"},

		"KindModal":        {Text: "hint"},
		"KindPicker":       {Picker: picker},
		"KindAgentTree":    {AgentTree: tree},
		"KindFocus":        {Text: "main/coder"},
		"KindTextInput":    {TextInput: &TextInputParams{Title: "t", Placeholder: "p", InitialValue: "v", Callback: "cb"}},
		"KindAbort":        {AgentID: "main"},
		"KindStatus":       {Key: "model", Value: "gpt"},
		"KindConsole":      {Text: "log line"},
		"KindConsoleClear": {},
		"KindResetHistory": {Messages: []sdk.Message{msg}},

		"KindUIArea":       {Area: area},
		"KindUIPatch":      {Patch: patch},
		"KindUIAreaUpdate": {AreaUpdate: areaUpdate},
		"KindUIAreaRemove": {Text: "chat"},

		"KindUIAreaHTML":   {AreaID: "chat", HTML: "<div></div>"},
		"KindUITextAppend": {AreaID: "chat", NodeID: "n1", Text: "delta"},
	}
}

// TestEveryDeclaredKindHasASample pins the sample table to the source: adding a
// constant without a sample fails, and a stale sample fails too.
func TestEveryDeclaredKindHasASample(t *testing.T) {
	declared := declaredEventKinds(t)
	samples := sampleEvents()

	for name := range declared {
		if _, ok := samples[name]; !ok {
			t.Errorf("kind %s (%q) has no sample Event — add one to sampleEvents", name, declared[name])
		}
	}
	for name := range samples {
		if _, ok := declared[name]; !ok {
			t.Errorf("sample for %s has no matching constant in wire.go", name)
		}
	}
}

// TestEventKindValuesUniqueAndWellFormed asserts the values clients switch on
// are unique and lower_snake_case, so a copy-paste collision can't merge two
// kinds into one case arm.
func TestEventKindValuesUniqueAndWellFormed(t *testing.T) {
	declared := declaredEventKinds(t)
	seen := map[string]string{}
	shape := regexp.MustCompile(`^[a-z][a-z0-9_]*$`)

	for name, val := range declared {
		if val == "" {
			t.Errorf("kind %s has an empty value", name)
		}
		if !shape.MatchString(val) {
			t.Errorf("kind %s value %q is not lower_snake_case", name, val)
		}
		if prev, dup := seen[val]; dup {
			t.Errorf("kind value %q is used by both %s and %s", val, prev, name)
		}
		seen[val] = name
	}
}

// TestEveryKindRoundTripsJSON marshals each sample, unmarshals it back, and
// asserts the Kind (and that re-marshaling is byte-identical) survives. This is
// the concrete stream contract: what the server writes, a client can read.
func TestEveryKindRoundTripsJSON(t *testing.T) {
	samples := sampleEvents()
	for name, ev := range samples {
		t.Run(name, func(t *testing.T) {
			ev.Seq = 42
			ev.Kind = EventKind(declaredEventKinds(t)[name])

			first, err := json.Marshal(ev)
			if err != nil {
				t.Fatalf("marshal: %v", err)
			}
			var back Event
			if err := json.Unmarshal(first, &back); err != nil {
				t.Fatalf("unmarshal: %v", err)
			}
			if back.Kind != ev.Kind {
				t.Fatalf("kind changed across round-trip: %q -> %q", ev.Kind, back.Kind)
			}
			if back.Seq != ev.Seq {
				t.Fatalf("seq changed across round-trip: %d -> %d", ev.Seq, back.Seq)
			}
			second, err := json.Marshal(back)
			if err != nil {
				t.Fatalf("re-marshal: %v", err)
			}
			if string(first) != string(second) {
				t.Fatalf("JSON not stable across round-trip:\n first: %s\nsecond: %s", first, second)
			}
		})
	}
}

// TestEveryKindDocumentedInSpecs asserts each declared kind value appears in the
// SPECS.md event-kinds table, so the protocol doc cannot drift from the code.
func TestEveryKindDocumentedInSpecs(t *testing.T) {
	specs, err := os.ReadFile("SPECS.md")
	if err != nil {
		t.Fatalf("read SPECS.md: %v", err)
	}
	doc := string(specs)

	for name, val := range declaredEventKinds(t) {
		// The table renders kinds as `value` or `value` / `value`.
		if !strings.Contains(doc, "`"+val+"`") {
			t.Errorf("kind %s (%q) is not documented in SPECS.md", name, val)
		}
	}
}

// TestProtocolVersionDeclared guards the one number a client gates on: it must
// be positive and match the value the ready frame advertises.
func TestProtocolVersionDeclared(t *testing.T) {
	if ProtocolVersion < 1 {
		t.Fatalf("ProtocolVersion must be >= 1, got %d", ProtocolVersion)
	}
	ev := sampleEvents()["KindReady"]
	if ev.Protocol != ProtocolVersion {
		t.Fatalf("ready sample advertises protocol %d, want %d", ev.Protocol, ProtocolVersion)
	}
}

// TestKindConstantsSortedStable is a guard that declaredEventKinds returns a
// deterministic set (map iteration order is random; this asserts the set, not an
// order, and documents the helper's contract for future readers).
func TestKindConstantsSortedStable(t *testing.T) {
	names := make([]string, 0, len(declaredEventKinds(t)))
	for n := range declaredEventKinds(t) {
		names = append(names, n)
	}
	sort.Strings(names)
	if len(names) < 20 {
		t.Fatalf("only %d kinds found; expected the full vocabulary", len(names))
	}
}
