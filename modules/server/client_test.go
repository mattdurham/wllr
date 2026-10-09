package server

// NOTE: Any changes to this file must be reflected in the corresponding SPECS.md or NOTES.md.

import (
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"

	"github.com/mattdurham/wllr/modules/agent"
	"github.com/mattdurham/wllr/modules/sdk"
)

// TestHandleClientServesPageWithProtocol proves GET / returns the self-contained
// front end and substitutes the live protocol version, so the page's version
// check matches wire.go without hard-coding a number in the HTML.
func TestHandleClientServesPageWithProtocol(t *testing.T) {
	srv := New(nil, Options{})
	rec := httptest.NewRecorder()
	srv.Handler().ServeHTTP(rec, httptest.NewRequest(http.MethodGet, "/", nil))

	if rec.Code != http.StatusOK {
		t.Fatalf("status = %d, want 200", rec.Code)
	}
	if ct := rec.Header().Get("Content-Type"); !strings.HasPrefix(ct, "text/html") {
		t.Fatalf("content-type = %q, want text/html", ct)
	}
	body := rec.Body.String()
	for _, want := range []string{
		"EventSource", "api/stream", "api/submit", "api/history", "api/scene",
		"api/ui/callback", "routeCallback", "api/models", "openModelPicker",
		"assistant_message",
		// the /context breakdown modal and the split picker
		"api/context", "contextbtn", "openContextModal", "splitPickerOverlay",
	} {
		if !strings.Contains(body, want) {
			t.Errorf("client page missing %q", want)
		}
	}
	if strings.Contains(body, "__PROTOCOL__") {
		t.Error("protocol placeholder was not substituted")
	}
	// The client inserts message bodies as server-rendered HTML (mdhtml.go) and
	// everything else as text. Guard the property that keeps that safe: the only
	// innerHTML sources are the server-provided html fields, and no raw
	// user/model text is ever interpolated into markup. The escaping itself is
	// proven by the mdhtml tests.
	if !strings.Contains(body, "body.innerHTML = html") {
		t.Error("client should render message bodies from the server-provided html")
	}
	for _, bad := range []string{
		"innerHTML = content", "innerHTML += content",
		"innerHTML = m.content", "innerHTML = f.text",
		"innerHTML = f.content", "innerHTML = text",
	} {
		if strings.Contains(body, bad) {
			t.Errorf("client must not interpolate raw message text into markup: %q", bad)
		}
	}
}

// TestHandleClientHasQueueAndToolPanes proves the page carries HTML equivalents
// of the terminal's queued-messages pane and tools pane: the sidebar containers,
// the clear button, and the functions that drive them from the same REST surface
// (/api/inbox, /api/inbox/clear) and event stream (tool_call/tool_result) the
// TUI uses. Without these the browser would silently lack two everyday surfaces.
func TestHandleClientHasQueueAndToolPanes(t *testing.T) {
	srv := New(nil, Options{})
	rec := httptest.NewRecorder()
	srv.Handler().ServeHTTP(rec, httptest.NewRequest(http.MethodGet, "/", nil))
	body := rec.Body.String()

	for _, want := range []string{
		"queue-pane", "queue-list", "queue-clear", "queue-count",
		"tool-pane", "tool-list",
		"api/inbox", "api/inbox/clear",
		"refreshInbox", "renderInbox", "clearInbox",
		"toolActivityBegin", "toolActivityEnd", "renderTools",
	} {
		if !strings.Contains(body, want) {
			t.Errorf("client page missing %q", want)
		}
	}
	// The queued pane renders message content as text, never as markup: a queued
	// message is user-supplied and must not be interpolated into innerHTML.
	for _, bad := range []string{"innerHTML = m.content", "innerHTML = oneLine"} {
		if strings.Contains(body, bad) {
			t.Errorf("queued pane must not interpolate message text into markup: %q", bad)
		}
	}
}

// TestHandleClientScriptParses extracts the embedded page's <script> body and
// runs `node --check` over it. Go cannot compile-check a string of JavaScript,
// so a syntax error in the front end would otherwise ship undetected and only
// surface as a blank page in a browser. The test skips when node is unavailable
// rather than failing the suite in environments without it.
func TestHandleClientScriptParses(t *testing.T) {
	node, err := exec.LookPath("node")
	if err != nil {
		t.Skip("node not available; skipping JavaScript syntax check")
	}

	srv := New(nil, Options{})
	rec := httptest.NewRecorder()
	srv.Handler().ServeHTTP(rec, httptest.NewRequest(http.MethodGet, "/", nil))

	body := rec.Body.String()
	start := strings.Index(body, "<script>")
	end := strings.Index(body, "</script>")
	if start < 0 || end < 0 || end < start {
		t.Fatalf("could not locate <script> block (start=%d end=%d)", start, end)
	}
	js := body[start+len("<script>") : end]

	dir := t.TempDir()
	path := filepath.Join(dir, "client.js")
	if err := os.WriteFile(path, []byte(js), 0o600); err != nil {
		t.Fatalf("write script: %v", err)
	}
	cmd := exec.Command(node, "--check", path) //nolint:gosec // node path resolved via LookPath; input is our own embedded page
	out, err := cmd.CombinedOutput()
	if err != nil {
		t.Fatalf("client JavaScript failed syntax check: %v\n%s", err, out)
	}
}

// TestHandleClientHasCommandAutocomplete proves the page carries an HTML
// equivalent of the terminal's slash-command autocomplete: the dropdown
// container, the command source (GET /api/commands), the same slash-word rule
// (slashWordAt), and the dispatch that routes a command to the callback
// endpoint. Without these the browser would lack the terminal's command
// discovery and its /commands would be sent as chat text.
func TestHandleClientHasCommandAutocomplete(t *testing.T) {
	srv := New(nil, Options{})
	rec := httptest.NewRecorder()
	srv.Handler().ServeHTTP(rec, httptest.NewRequest(http.MethodGet, "/", nil))
	body := rec.Body.String()

	for _, want := range []string{
		"id=\"suggest\"", "api/commands", "refreshCommands",
		"slashWordAt", "updateSuggestions", "completeSuggestion", "moveSuggestion",
		"runCommand", "localCommands", "openHelp",
	} {
		if !strings.Contains(body, want) {
			t.Errorf("client page missing %q", want)
		}
	}
	// The dropdown must filter by command name and route the terminal's ids.
	for _, want := range []string{"__wllr:model", "__wllr:thinking", "__wllr:speed"} {
		if !strings.Contains(body, want) {
			t.Errorf("command dispatch missing callback %q", want)
		}
	}
}

// TestHandleClientRejectsUnknownPath proves only "/" serves the page, so the
// catch-all route cannot shadow a missing API path into an HTML response.
func TestHandleClientRejectsUnknownPath(t *testing.T) {
	srv := New(nil, Options{})
	rec := httptest.NewRecorder()
	srv.Handler().ServeHTTP(rec, httptest.NewRequest(http.MethodGet, "/nope", nil))
	if rec.Code != http.StatusNotFound {
		t.Fatalf("status = %d, want 404", rec.Code)
	}
}

// TestHandleSceneSnapshot proves GET /api/scene returns the areas the sink has
// accumulated, with rendered HTML, so a reconnecting client can seed its view.
func TestHandleSceneSnapshot(t *testing.T) {
	srv := New(nil, Options{})
	sink := srv.HubSink()

	if err := sink.SceneCreate(sdk.UIArea{ID: "chat", Placement: sdk.UIAreaMain}); err != nil {
		t.Fatalf("create: %v", err)
	}
	if err := sink.ScenePatch(sdk.UIPatchParams{Area: "chat", Ops: []sdk.UIPatchOp{
		{Op: sdk.UIOpSetRoot, Node: &sdk.UINode{ID: "root", Type: sdk.UINodeVStack, Children: []sdk.UINode{
			{ID: "m", Type: sdk.UINodeText, Text: "transcript goes here"},
		}}},
	}}); err != nil {
		t.Fatalf("patch: %v", err)
	}

	rec := httptest.NewRecorder()
	srv.Handler().ServeHTTP(rec, httptest.NewRequest(http.MethodGet, "/api/scene", nil))
	if rec.Code != http.StatusOK {
		t.Fatalf("status = %d, want 200", rec.Code)
	}
	var body struct {
		Protocol int        `json:"protocol"`
		Areas    []AreaView `json:"areas"`
	}
	if err := json.Unmarshal(rec.Body.Bytes(), &body); err != nil {
		t.Fatalf("decode: %v", err)
	}
	if body.Protocol != ProtocolVersion {
		t.Errorf("protocol = %d, want %d", body.Protocol, ProtocolVersion)
	}
	if len(body.Areas) != 1 || body.Areas[0].Area.ID != "chat" {
		t.Fatalf("areas = %+v, want one chat area", body.Areas)
	}
	if !strings.Contains(body.Areas[0].HTML, "transcript goes here") {
		t.Errorf("snapshot HTML missing content: %q", body.Areas[0].HTML)
	}
}

// TestHandleSceneRejectsNonGet proves the endpoint is read-only.
func TestHandleSceneRejectsNonGet(t *testing.T) {
	srv := New(nil, Options{})
	rec := httptest.NewRecorder()
	srv.Handler().ServeHTTP(rec, httptest.NewRequest(http.MethodPost, "/api/scene", nil))
	if rec.Code != http.StatusMethodNotAllowed {
		t.Fatalf("status = %d, want 405", rec.Code)
	}
}

// TestSceneFramesReachStream proves the end-to-end path a browser relies on:
// an extension scene call publishes the rendered HTML frame on the SSE stream,
// through the same HubSink serve mode installs.
func TestSceneFramesReachStream(t *testing.T) {
	pool, _ := newTestPool(t)
	srv := New(pool, Options{})
	_ = pool // pool only needed so the server is fully wired
	sink := srv.HubSink()

	ch, _, cancel := srv.Hub().Subscribe(0)
	defer cancel()

	if err := sink.SceneCreate(sdk.UIArea{ID: "status", Placement: sdk.UIAreaStatus}); err != nil {
		t.Fatalf("create: %v", err)
	}
	ev := nextEvent(t, ch)
	if ev.Kind != KindUIArea {
		t.Fatalf("first frame = %s, want ui_area", ev.Kind)
	}
	ev = nextEvent(t, ch)
	if ev.Kind != KindUIAreaHTML || !strings.Contains(ev.HTML, `data-area-id="status"`) {
		t.Fatalf("second frame = %+v, want ui_area_html for status", ev)
	}
	_ = agent.MainAgentID
}
