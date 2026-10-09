package server

// NOTE: Any changes to this file must be reflected in the corresponding SPECS.md or NOTES.md.

import (
	"bytes"
	"io"
	"log/slog"
	"net/http"
	"net/http/httptest"
	"net/url"
	"strings"
	"sync"
	"testing"
)

// proxyPeer records what a fake peer's API surface received, so the proxy tests
// assert the forwarded request rather than a mock call.
type proxyPeer struct {
	mu       sync.Mutex
	path     string
	rawQuery string
	auth     string
	method   string
	body     string
	requests int
}

func (p *proxyPeer) serve(w http.ResponseWriter, r *http.Request) {
	body, _ := io.ReadAll(r.Body)
	p.mu.Lock()
	p.requests++
	p.path = r.URL.Path
	p.rawQuery = r.URL.RawQuery
	p.auth = r.Header.Get("Authorization")
	p.method = r.Method
	p.body = string(body)
	p.mu.Unlock()
	w.Header().Set("Content-Type", "application/json")
	_, _ = io.WriteString(w, p.body)
}

func (p *proxyPeer) snapshot() proxyPeer {
	p.mu.Lock()
	defer p.mu.Unlock()
	return proxyPeer{
		path: p.path, rawQuery: p.rawQuery, auth: p.auth,
		method: p.method, body: p.body, requests: p.requests,
	}
}

func newProxyPeer(t *testing.T, rec *proxyPeer) *httptest.Server {
	t.Helper()
	srv := httptest.NewServer(http.HandlerFunc(rec.serve))
	t.Cleanup(srv.Close)
	return srv
}

// proxyServer builds a server fronting one peer with the given token.
func proxyServer(t *testing.T, rec *proxyPeer, token string) (*Server, *httptest.Server) {
	t.Helper()
	peer := newProxyPeer(t, rec)
	srv := New(nil, Options{Peers: []Peer{{ID: "ci", BaseURL: peer.URL, Token: token}}})
	return srv, peer
}

// TestPeerProxyForwardsAPIPathQueryAndToken proves the rewrite: the
// /api/instances/{id} prefix is stripped, the query is preserved, and the
// inbound origin credential is replaced by the peer's own token.
func TestPeerProxyForwardsAPIPathQueryAndToken(t *testing.T) {
	rec := &proxyPeer{}
	srv, _ := proxyServer(t, rec, "peer-secret")

	req := httptest.NewRequest(http.MethodGet, "/api/instances/ci/api/history?agent=main", nil)
	req.Header.Set("Authorization", "Bearer origin-secret")
	rr := httptest.NewRecorder()
	srv.Handler().ServeHTTP(rr, req)
	if rr.Code != http.StatusOK {
		t.Fatalf("status = %d, want 200", rr.Code)
	}
	got := rec.snapshot()
	if got.path != "/api/history" {
		t.Errorf("forwarded path = %q, want /api/history", got.path)
	}
	if !strings.Contains(got.rawQuery, "agent=main") {
		t.Errorf("forwarded query = %q, want agent=main", got.rawQuery)
	}
	if got.auth != "Bearer peer-secret" {
		t.Errorf("forwarded Authorization = %q, want the peer token", got.auth)
	}
}

// TestPeerProxyDropsOriginTokenQuery covers a leak the origin's own auth opens:
// it accepts ?token=, so forwarding RawQuery verbatim would relay the origin's
// credential to the peer. The inbound token must be deleted; every other
// parameter survives.
func TestPeerProxyDropsOriginTokenQuery(t *testing.T) {
	rec := &proxyPeer{}
	srv, _ := proxyServer(t, rec, "")

	req := httptest.NewRequest(http.MethodGet, "/api/instances/ci/api/health?token=origin-secret&x=1", nil)
	rr := httptest.NewRecorder()
	srv.Handler().ServeHTTP(rr, req)
	if rr.Code != http.StatusOK {
		t.Fatalf("status = %d, want 200", rr.Code)
	}
	q, err := url.ParseQuery(rec.snapshot().rawQuery)
	if err != nil {
		t.Fatalf("parse forwarded query: %v", err)
	}
	if q.Has("token") {
		t.Errorf("inbound token forwarded to peer: %q", rec.snapshot().rawQuery)
	}
	if q.Get("x") != "1" {
		t.Errorf("forwarded query = %q, want x=1 preserved", rec.snapshot().rawQuery)
	}
}

// TestPeerProxyForwardsPostBody proves the body is forwarded verbatim, which
// submit/cancel/inbox/callback all depend on, and the peer's response returns
// through unchanged.
func TestPeerProxyForwardsPostBody(t *testing.T) {
	rec := &proxyPeer{}
	srv, _ := proxyServer(t, rec, "")

	body := `{"agent":"main","content":"hi"}`
	req := httptest.NewRequest(http.MethodPost, "/api/instances/ci/api/submit", strings.NewReader(body))
	rr := httptest.NewRecorder()
	srv.Handler().ServeHTTP(rr, req)
	if rr.Code != http.StatusOK {
		t.Fatalf("status = %d, want 200", rr.Code)
	}
	if got := rec.snapshot().body; got != body {
		t.Errorf("peer received body %q, want %q", got, body)
	}
	if got := rr.Body.String(); got != body {
		t.Errorf("response body = %q, want the peer's echo %q", got, body)
	}
}

// TestPeerProxyStreamsSSE is the flush regression guard: the peer writes two SSE
// frames and flushes, and the origin's response must carry both frames with
// Flushed set (httptest.ResponseRecorder sets Flushed only when Flush is
// actually called — the same class of regression as
// TestServerStreamReplaysBacklogFromSince).
func TestPeerProxyStreamsSSE(t *testing.T) {
	peer := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		w.Header().Set("Content-Type", "text/event-stream")
		w.WriteHeader(http.StatusOK)
		f, ok := w.(http.Flusher)
		if !ok {
			return
		}
		_, _ = io.WriteString(w, "data: one\n\n")
		f.Flush()
		_, _ = io.WriteString(w, "data: two\n\n")
		f.Flush()
	}))
	defer peer.Close()

	srv := New(nil, Options{Peers: []Peer{{ID: "ci", BaseURL: peer.URL}}})
	rr := httptest.NewRecorder()
	srv.Handler().ServeHTTP(rr, httptest.NewRequest(http.MethodGet, "/api/instances/ci/api/stream", nil))

	body := rr.Body.String()
	if !strings.Contains(body, "data: one") || !strings.Contains(body, "data: two") {
		t.Errorf("SSE frames missing from proxied stream: %q", body)
	}
	if !rr.Flushed {
		t.Error("proxied SSE stream was not flushed (buffered response)")
	}
}

// TestPeerProxyReturns502WhenPeerStalls proves the proxy does not hang the view
// on a peer that accepts the TCP connection but never writes response headers
// (the ResponseHeaderTimeout budget). Without it, http.DefaultTransport has no
// header timeout and the proxied request — and the page's EventSource — would
// block until the inbound client gave up, with no error surfacing.
func TestPeerProxyReturns502WhenPeerStalls(t *testing.T) {
	release := make(chan struct{})
	peer := httptest.NewServer(http.HandlerFunc(func(http.ResponseWriter, *http.Request) {
		<-release // accept the connection, never write headers
	}))
	t.Cleanup(func() { close(release); peer.Close() })

	srv := New(nil, Options{Peers: []Peer{{ID: "ci", BaseURL: peer.URL}}})
	rr := httptest.NewRecorder()
	srv.Handler().ServeHTTP(rr, httptest.NewRequest(http.MethodGet, "/api/instances/ci/api/health", nil))
	if rr.Code != http.StatusBadGateway {
		t.Fatalf("status = %d, want 502 (a stalled peer must not hang the view)", rr.Code)
	}
	if !strings.Contains(rr.Body.String(), "instance unreachable") {
		t.Errorf("502 body = %q, want it to name the instance as unreachable", rr.Body.String())
	}
}

// TestPeerProxyUnknownInstance404 proves only a known peer id is proxied, and
// the self instance — served in-process — is deliberately not proxiable.
func TestPeerProxyUnknownInstance404(t *testing.T) {
	rec := &proxyPeer{}
	srv, _ := proxyServer(t, rec, "")

	for _, path := range []string{
		"/api/instances/nope/api/health",
		"/api/instances/local/api/health",
	} {
		rr := httptest.NewRecorder()
		srv.Handler().ServeHTTP(rr, httptest.NewRequest(http.MethodGet, path, nil))
		if rr.Code != http.StatusNotFound {
			t.Errorf("GET %s = %d, want 404", path, rr.Code)
		}
	}
	if got := rec.snapshot().requests; got != 0 {
		t.Errorf("peer saw %d requests for unknown instances, want 0", got)
	}
}

// TestPeerProxyRejectsNonAPIPath proves the route is not an open proxy: a path
// outside /api/instances/{id}/api/ never reaches the peer.
func TestPeerProxyRejectsNonAPIPath(t *testing.T) {
	rec := &proxyPeer{}
	srv, _ := proxyServer(t, rec, "")

	for _, path := range []string{
		"/api/instances/ci/secret",
		"/api/instances/ci/",
	} {
		rr := httptest.NewRecorder()
		srv.Handler().ServeHTTP(rr, httptest.NewRequest(http.MethodGet, path, nil))
		if rr.Code != http.StatusNotFound {
			t.Errorf("GET %s = %d, want 404", path, rr.Code)
		}
	}
	if got := rec.snapshot().requests; got != 0 {
		t.Errorf("peer saw %d requests for non-API paths, want 0", got)
	}
}

// TestPeerProxyReturns502WhenPeerDown proves a down peer is a clean per-instance
// fault (502), not a hang or a 200 with an empty body.
func TestPeerProxyReturns502WhenPeerDown(t *testing.T) {
	dead := httptest.NewServer(http.HandlerFunc(func(http.ResponseWriter, *http.Request) {}))
	dead.Close()

	srv := New(nil, Options{Peers: []Peer{{ID: "ci", BaseURL: dead.URL}}})
	rr := httptest.NewRecorder()
	srv.Handler().ServeHTTP(rr, httptest.NewRequest(http.MethodGet, "/api/instances/ci/api/health", nil))
	if rr.Code != http.StatusBadGateway {
		t.Fatalf("status = %d, want 502", rr.Code)
	}
	if !strings.Contains(rr.Body.String(), "instance unreachable") {
		t.Errorf("502 body = %q, want it to name the instance as unreachable", rr.Body.String())
	}
}

// TestPeerProxyStripsPeerLocationOrigin proves a 3xx Location from a peer never
// carries the peer's scheme/host (or any other cross-origin target) to the
// browser (invariant 12): an absolute, protocol-relative, backslash, or opaque
// Location is reduced to a same-origin path, while an already-relative path is
// left alone.
func TestPeerProxyStripsPeerLocationOrigin(t *testing.T) {
	cases := []struct{ name, peer, want string }{
		{"absolute", "http://evil.example/x?y=1", "/x?y=1"},
		{"protocol-relative", "//evil.example/x", "/x"},
		{"backslash-with-slash", "/\\evil.example/x", "/%5Cevil.example/x"},
		{"backslash", "\\\\evil.example/x", "/%5C%5Cevil.example/x"},
		{"opaque-scheme", "javascript:alert(1)", "/"},
		{"relative-unchanged", "/relative?q=1", "/relative?q=1"},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			loc := tc.peer
			peer := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
				w.Header().Set("Location", loc)
				w.WriteHeader(http.StatusFound)
			}))
			t.Cleanup(peer.Close)

			srv := New(nil, Options{Peers: []Peer{{ID: "ci", BaseURL: peer.URL}}})
			rr := httptest.NewRecorder()
			srv.Handler().ServeHTTP(rr, httptest.NewRequest(http.MethodGet, "/api/instances/ci/api/x", nil))
			if rr.Code != http.StatusFound {
				t.Fatalf("status = %d, want 302", rr.Code)
			}
			got := rr.Header().Get("Location")
			if got != tc.want {
				t.Errorf("Location = %q, want %q", got, tc.want)
			}
			// The browser is meant to trust only the origin, so the rewritten
			// Location must be a same-origin absolute path: no scheme, no
			// authority, no backslash.
			if !strings.HasPrefix(got, "/") || strings.HasPrefix(got, "//") {
				t.Errorf("Location = %q is not a same-origin absolute path", got)
			}
			if ru, err := url.Parse(got); err != nil || ru.Scheme != "" || ru.Host != "" {
				t.Errorf("Location = %q still carries a scheme or authority", got)
			}
		})
	}
}

// TestPeerProxyStripsPeerSetCookie proves a peer's Set-Cookie is not forwarded to
// the browser: the response is delivered under the origin's origin, so a peer
// must not be able to plant origin-scoped state (invariant 12).
func TestPeerProxyStripsPeerSetCookie(t *testing.T) {
	peer := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Set-Cookie", "k=v; Path=/")
		w.Header().Set("X-Peer", "kept")
		w.WriteHeader(http.StatusOK)
	}))
	t.Cleanup(peer.Close)

	srv := New(nil, Options{Peers: []Peer{{ID: "ci", BaseURL: peer.URL}}})
	rr := httptest.NewRecorder()
	srv.Handler().ServeHTTP(rr, httptest.NewRequest(http.MethodGet, "/api/instances/ci/api/x", nil))
	if rr.Code != http.StatusOK {
		t.Fatalf("status = %d, want 200", rr.Code)
	}
	if got := rr.Header().Values("Set-Cookie"); len(got) != 0 {
		t.Errorf("Set-Cookie = %q, want it stripped", got)
	}
	if got := rr.Header().Get("X-Peer"); got != "kept" {
		t.Errorf("X-Peer = %q, want ordinary headers still forwarded", got)
	}
}

// TestPeerProxyLogsFailure proves a proxy failure reaches the operator's log with
// the peer id, while the browser body stays generic.
func TestPeerProxyLogsFailure(t *testing.T) {
	dead := httptest.NewServer(http.HandlerFunc(func(http.ResponseWriter, *http.Request) {}))
	dead.Close()

	var buf bytes.Buffer
	logger := slog.New(slog.NewTextHandler(&buf, &slog.HandlerOptions{Level: slog.LevelWarn}))
	srv := New(nil, Options{Logger: logger, Peers: []Peer{{ID: "ci", BaseURL: dead.URL}}})
	rr := httptest.NewRecorder()
	srv.Handler().ServeHTTP(rr, httptest.NewRequest(http.MethodGet, "/api/instances/ci/api/health", nil))
	if rr.Code != http.StatusBadGateway {
		t.Fatalf("status = %d, want 502", rr.Code)
	}
	logged := buf.String()
	if !strings.Contains(logged, "peer proxy failed") || !strings.Contains(logged, "ci") {
		t.Errorf("log = %q, want a peer-proxy failure naming ci", logged)
	}
}

// TestPeerProxyForwardsPathToPeerBase proves the /api/instances/{id} prefix is
// stripped *before* the base path is joined, so a peer whose BaseURL carries a
// path (http://host/wllr, e.g. behind a path-prefixed ingress) still receives
// /wllr/api/history — the same URL the health probe (peerAPIURL) resolves.
// Trimming after SetURL left /wllr/api/instances/ci/api/history, so the peer
// 404'd while the probe reported it healthy (L2).
func TestPeerProxyForwardsPathToPeerBase(t *testing.T) {
	rec := &proxyPeer{}
	peer := newProxyPeer(t, rec)
	srv := New(nil, Options{Peers: []Peer{{ID: "ci", BaseURL: peer.URL + "/wllr"}}})

	rr := httptest.NewRecorder()
	srv.Handler().ServeHTTP(rr, httptest.NewRequest(http.MethodGet, "/api/instances/ci/api/history", nil))
	if rr.Code != http.StatusOK {
		t.Fatalf("status = %d, want 200", rr.Code)
	}
	if got := rec.snapshot().path; got != "/wllr/api/history" {
		t.Errorf("forwarded path = %q, want /wllr/api/history (prefix stripped before the base join)", got)
	}
}

// TestPeerAPIURLJoinsBasePath pins the probe helper's own URL resolution: a peer
// BaseURL with a path (`http://host/wllr`) or trailing slash resolves to the
// same effective URL the proxy's SetURL produces. It exercises only peerAPIURL
// (the probe path); TestPeerProxyForwardsPathToPeerBase covers the proxy half.
func TestPeerAPIURLJoinsBasePath(t *testing.T) {
	cases := []struct{ base, apiPath, want string }{
		{"http://host", "/api/health", "http://host/api/health"},
		{"http://host/", "/api/health", "http://host/api/health"},
		{"http://host/wllr", "/api/health", "http://host/wllr/api/health"},
		{"http://host/wllr/", "/api/health", "http://host/wllr/api/health"},
	}
	for _, tc := range cases {
		got, err := peerAPIURL(Peer{ID: "ci", BaseURL: tc.base}, tc.apiPath)
		if err != nil {
			t.Fatalf("peerAPIURL(%q): %v", tc.base, err)
		}
		if got != tc.want {
			t.Errorf("peerAPIURL(%q) = %q, want %q", tc.base, got, tc.want)
		}
	}
}

// TestServerAuthCoversInstanceRoutes extends server SPECS invariant 6 to the new
// routes: both sit on s.mux, so the authenticate wrapper guards them like any
// other route.
func TestServerAuthCoversInstanceRoutes(t *testing.T) {
	rec := &proxyPeer{}
	peer := newProxyPeer(t, rec)
	srv := New(nil, Options{Token: "t", Peers: []Peer{{ID: "ci", BaseURL: peer.URL}}})

	for _, path := range []string{"/api/instances", "/api/instances/ci/api/health"} {
		rr := httptest.NewRecorder()
		srv.Handler().ServeHTTP(rr, httptest.NewRequest(http.MethodGet, path, nil))
		if rr.Code != http.StatusUnauthorized {
			t.Errorf("GET %s without a token = %d, want 401", path, rr.Code)
		}
	}
}
