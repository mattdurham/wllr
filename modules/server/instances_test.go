package server

// NOTE: Any changes to this file must be reflected in the corresponding SPECS.md or NOTES.md.

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"net/http/httptest"
	"net/url"
	"strings"
	"sync"
	"testing"
)

// instancesResponse is the GET /api/instances body decoded for assertions.
type instancesResponse struct {
	Self      string         `json:"self"`
	Instances []instanceView `json:"instances"`
}

// getInstances drives GET /api/instances through the fully wrapped handler and
// decodes the body, so tests assert the same contract a browser sees.
func getInstances(t *testing.T, srv *Server) *httptest.ResponseRecorder {
	t.Helper()
	rec := httptest.NewRecorder()
	srv.Handler().ServeHTTP(rec, httptest.NewRequest(http.MethodGet, "/api/instances", nil))
	return rec
}

// TestHandleInstancesDefaultsSelfID pins the contract's `self` field and the
// default single-entry list: with no peers configured, GET /api/instances
// reports this origin alone, in process, with the live protocol version.
func TestHandleInstancesDefaultsSelfID(t *testing.T) {
	srv := New(nil, Options{})
	rec := getInstances(t, srv)

	if rec.Code != http.StatusOK {
		t.Fatalf("status = %d, want 200", rec.Code)
	}
	var body instancesResponse
	if err := json.Unmarshal(rec.Body.Bytes(), &body); err != nil {
		t.Fatalf("decode: %v", err)
	}
	if body.Self != DefaultSelfID {
		t.Errorf("self = %q, want %q", body.Self, DefaultSelfID)
	}
	if len(body.Instances) != 1 {
		t.Fatalf("instances = %+v, want exactly the self entry", body.Instances)
	}
	got := body.Instances[0]
	if got.ID != DefaultSelfID || !got.Self || got.Status != statusOK {
		t.Errorf("self entry = %+v, want id=%q self=true status=%q", got, DefaultSelfID, statusOK)
	}
	if got.Protocol != ProtocolVersion {
		t.Errorf("self protocol = %d, want %d", got.Protocol, ProtocolVersion)
	}
}

// TestHandleInstancesRejectsNonGet matches every other handler's method guard.
func TestHandleInstancesRejectsNonGet(t *testing.T) {
	srv := New(nil, Options{})
	rec := httptest.NewRecorder()
	srv.Handler().ServeHTTP(rec, httptest.NewRequest(http.MethodPost, "/api/instances", nil))
	if rec.Code != http.StatusMethodNotAllowed {
		t.Fatalf("status = %d, want 405", rec.Code)
	}
}

// peerRecorder is a fake peer's /api/health endpoint: it records the bearer
// token it received and how many times it was probed, so tests can assert
// per-request auth and the probe TTL cache.
type peerRecorder struct {
	mu       sync.Mutex
	auth     string
	hits     int
	protocol int
	lastSeq  int64
}

func (p *peerRecorder) serveHealth(w http.ResponseWriter, r *http.Request) {
	p.mu.Lock()
	p.auth = r.Header.Get("Authorization")
	p.hits++
	p.mu.Unlock()
	w.Header().Set("Content-Type", "application/json")
	_ = json.NewEncoder(w).Encode(map[string]any{
		"ok":       true,
		"protocol": p.protocol,
		"last_seq": p.lastSeq,
	})
}

func (p *peerRecorder) state() (auth string, hits int) {
	p.mu.Lock()
	defer p.mu.Unlock()
	return p.auth, p.hits
}

// newPeerServer starts a peer whose /api/health answers with protocol/lastSeq.
func newPeerServer(t *testing.T, rec *peerRecorder) *httptest.Server {
	t.Helper()
	srv := httptest.NewServer(http.HandlerFunc(rec.serveHealth))
	t.Cleanup(srv.Close)
	return srv
}

// TestHandleInstancesListsSelfAndPeers pins the list shape: self first, then the
// peer with the health it reported. It also proves the probe authenticates per
// request and that the response leaks neither the peer URL nor its token.
func TestHandleInstancesListsSelfAndPeers(t *testing.T) {
	rec := &peerRecorder{protocol: 1, lastSeq: 45}
	peer := newPeerServer(t, rec)
	srv := New(nil, Options{Peers: []Peer{{ID: "ci", BaseURL: peer.URL, Token: "peer-secret"}}})

	resp := getInstances(t, srv)
	if resp.Code != http.StatusOK {
		t.Fatalf("status = %d, want 200", resp.Code)
	}
	var body instancesResponse
	if err := json.Unmarshal(resp.Body.Bytes(), &body); err != nil {
		t.Fatalf("decode: %v", err)
	}
	if body.Self != DefaultSelfID {
		t.Errorf("self = %q, want %q", body.Self, DefaultSelfID)
	}
	if len(body.Instances) != 2 {
		t.Fatalf("instances = %+v, want self + peer", body.Instances)
	}
	if got := body.Instances[0]; got.ID != DefaultSelfID || !got.Self {
		t.Errorf("entry 0 = %+v, want self first", got)
	}
	got := body.Instances[1]
	if got.ID != "ci" || got.Self || got.Status != statusOK || got.Protocol != 1 || got.LastSeq != 45 {
		t.Errorf("peer entry = %+v, want ci/ok/protocol 1/last_seq 45", got)
	}
	if auth, _ := rec.state(); auth != "Bearer peer-secret" {
		t.Errorf("probe Authorization = %q, want %q", auth, "Bearer peer-secret")
	}
	raw := resp.Body.String()
	if strings.Contains(raw, peer.URL) {
		t.Errorf("response leaked the peer URL: %s", raw)
	}
	if strings.Contains(raw, "peer-secret") {
		t.Errorf("response leaked the peer token: %s", raw)
	}
}

// TestHandleInstancesReportsPeerDown proves a dead peer is a clean per-instance
// fault: the list still answers 200, that entry is down with an error, and a
// healthy peer in the same request is unaffected (failure isolation).
func TestHandleInstancesReportsPeerDown(t *testing.T) {
	dead := httptest.NewServer(http.HandlerFunc(func(http.ResponseWriter, *http.Request) {}))
	dead.Close()

	rec := &peerRecorder{protocol: 1, lastSeq: 7}
	healthy := newPeerServer(t, rec)
	srv := New(nil, Options{Peers: []Peer{
		{ID: "down", BaseURL: dead.URL},
		{ID: "ci", BaseURL: healthy.URL},
	}})

	resp := getInstances(t, srv)
	if resp.Code != http.StatusOK {
		t.Fatalf("status = %d, want 200 even with a down peer", resp.Code)
	}
	var body instancesResponse
	if err := json.Unmarshal(resp.Body.Bytes(), &body); err != nil {
		t.Fatalf("decode: %v", err)
	}
	if len(body.Instances) != 3 {
		t.Fatalf("instances = %+v, want self + two peers", body.Instances)
	}
	down := body.Instances[1]
	if down.ID != "down" || down.Status != statusDown || down.Error == "" {
		t.Errorf("down entry = %+v, want status=down with an error", down)
	}
	if up := body.Instances[2]; up.ID != "ci" || up.Status != statusOK {
		t.Errorf("healthy peer after a down one = %+v, want ok", up)
	}
}

// TestHandleInstancesErrorOmitsPeerTopology pins invariant 12 for the down
// case: the probe's raw net/http error embeds the peer's URL/host/port, so the
// reported `error` must be a sanitized cause that names the failure class and
// leaks none of the topology.
func TestHandleInstancesErrorOmitsPeerTopology(t *testing.T) {
	dead := httptest.NewServer(http.HandlerFunc(func(http.ResponseWriter, *http.Request) {}))
	hostPort := strings.TrimPrefix(dead.URL, "http://")
	dead.Close()

	srv := New(nil, Options{Peers: []Peer{{ID: "down", BaseURL: dead.URL}}})
	resp := getInstances(t, srv)
	if resp.Code != http.StatusOK {
		t.Fatalf("status = %d, want 200", resp.Code)
	}
	var body instancesResponse
	if err := json.Unmarshal(resp.Body.Bytes(), &body); err != nil {
		t.Fatalf("decode: %v", err)
	}
	if len(body.Instances) != 2 {
		t.Fatalf("instances = %+v, want self + down peer", body.Instances)
	}
	down := body.Instances[1]
	if down.Status != statusDown || down.Error == "" {
		t.Fatalf("down entry = %+v, want status=down with an error", down)
	}
	if down.Error != "unreachable" {
		t.Errorf("error = %q, want %q (the failure class)", down.Error, "unreachable")
	}
	raw := resp.Body.String()
	for _, leaked := range []string{"http://", hostPort, dead.URL} {
		if strings.Contains(raw, leaked) {
			t.Errorf("response leaked peer topology %q: %s", leaked, raw)
		}
	}
}

// TestProbeCauseClassifiesWithoutTopology proves the classifier keeps the
// failure class ('timeout' vs 'unreachable') while dropping the URL/host/port a
// raw net/http error carries.
func TestProbeCauseClassifiesWithoutTopology(t *testing.T) {
	cases := []struct {
		name string
		err  error
		want string
	}{
		{
			name: "connection refused",
			err: &url.Error{
				Op:  "Get",
				URL: "http://127.0.0.1:8766/api/health",
				Err: errors.New("dial tcp 127.0.0.1:8766: connect: connection refused"),
			},
			want: "unreachable",
		},
		{
			name: "deadline exceeded",
			err:  &url.Error{Op: "Get", URL: "http://127.0.0.1:8766/api/health", Err: context.DeadlineExceeded},
			want: "timeout",
		},
		{
			name: "wrapped timeout",
			err:  fmt.Errorf("probe: %w", context.DeadlineExceeded),
			want: "timeout",
		},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			got := probeCause(tc.err)
			if got != tc.want {
				t.Errorf("probeCause(%v) = %q, want %q", tc.err, got, tc.want)
			}
			if strings.Contains(got, "http") || strings.Contains(got, "127.0.0.1") {
				t.Errorf("probeCause leaked topology: %q", got)
			}
		})
	}
}

// TestProbeOnceReportsNon200Status pins that a non-200 peer answer is reported
// as the bare status code, which is safe to show: it reveals no topology.
func TestProbeOnceReportsNon200Status(t *testing.T) {
	peer := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		w.WriteHeader(http.StatusUnauthorized)
	}))
	t.Cleanup(peer.Close)
	h := newInstanceProbe().probeOnce(context.Background(), Peer{ID: "ci", BaseURL: peer.URL})
	if h.ok {
		t.Fatalf("probe reported ok against a 401 peer")
	}
	if h.err != "http 401" {
		t.Errorf("err = %q, want %q", h.err, "http 401")
	}
}

// TestProbeRejectsNonWllr200 pins that a 200 from something other than a wllr
// health endpoint is not treated as healthy: the body must actually say so
// (`ok:true` and a non-zero protocol), or a foreign service on a mistyped port
// would report as up.
func TestProbeRejectsNonWllr200(t *testing.T) {
	peer := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		_, _ = io.WriteString(w, `{"hello":"world"}`)
	}))
	t.Cleanup(peer.Close)
	h := newInstanceProbe().probeOnce(context.Background(), Peer{ID: "ci", BaseURL: peer.URL})
	if h.ok {
		t.Fatalf("probe reported ok for a non-wllr 200 peer")
	}
	if h.err != "malformed response" {
		t.Errorf("err = %q, want %q", h.err, "malformed response")
	}
}

// canceledTransport is an http.RoundTripper that always fails with
// context.Canceled, so a probe attempt can be forced into the indeterminate
// (cancelled) path deterministically.
type canceledTransport struct{}

func (canceledTransport) RoundTrip(*http.Request) (*http.Response, error) {
	return nil, context.Canceled
}

// TestProbeCanceledRequestDoesNotCacheDown pins M2: a probe that is cancelled (a
// client aborting GET /api/instances) must not leave a false "down" in the cache
// for the TTL, and a cancelled request context must not cancel a probe that can
// still complete against a healthy peer.
func TestProbeCanceledRequestDoesNotCacheDown(t *testing.T) {
	// A cancelled probe attempt is indeterminate and must not be cached.
	p := &instanceProbe{client: &http.Client{Transport: canceledTransport{}}, cached: make(map[string]cachedHealth)}
	got := p.view(context.Background(), Peer{ID: "ci", BaseURL: "http://127.0.0.1:1"})
	if got.Status != statusDown {
		t.Errorf("cancelled probe view = %+v, want down for this call", got)
	}
	if _, ok := p.lookupHealth("ci"); ok {
		t.Errorf("a cancelled probe was cached as down")
	}

	// A cancelled *request* context must not poison a probe against a healthy
	// peer: the probe runs on a request-independent base context, so the peer's
	// real health is reported and cached.
	rec := &peerRecorder{protocol: 1, lastSeq: 9}
	peer := newPeerServer(t, rec)
	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	q := newInstanceProbe()
	got = q.view(ctx, Peer{ID: "ci", BaseURL: peer.URL})
	if got.Status != statusOK || got.Protocol != 1 || got.LastSeq != 9 {
		t.Errorf("cancelled-request probe = %+v, want ok/protocol 1/last_seq 9", got)
	}
	if h, ok := q.lookupHealth("ci"); !ok || !h.ok {
		t.Errorf("cached health = %+v (ok=%v), want a cached ok", h, ok)
	}
}

// TestHandleInstancesCachesPeerProbe pins the TTL: two successive polls must
// not hammer a peer, so the result is cached for probeTTL.
func TestHandleInstancesCachesPeerProbe(t *testing.T) {
	rec := &peerRecorder{protocol: 1, lastSeq: 1}
	peer := newPeerServer(t, rec)
	srv := New(nil, Options{Peers: []Peer{{ID: "ci", BaseURL: peer.URL}}})

	getInstances(t, srv)
	getInstances(t, srv)
	if _, hits := rec.state(); hits != 1 {
		t.Errorf("peer health hits = %d, want 1 (TTL cache)", hits)
	}
}

// TestHandleInstancesSkipsInvalidPeers proves startup survives a bad peer table
// and no phantom instance reaches the selector: empty ids, ids outside the
// routable charset, duplicates, a self-collision, and an unparseable URL are
// dropped, and a dropped id is not proxiable (404).
func TestHandleInstancesSkipsInvalidPeers(t *testing.T) {
	rec := &peerRecorder{protocol: 1, lastSeq: 1}
	ok := newPeerServer(t, rec)
	srv := New(nil, Options{SelfID: "local", Peers: []Peer{
		{ID: "bad", BaseURL: "://nope"},
		{ID: "", BaseURL: "http://127.0.0.1:1"},
		{ID: "bad/id", BaseURL: ok.URL},
		{ID: "has space", BaseURL: ok.URL},
		{ID: "ci", BaseURL: ok.URL},
		{ID: "ci", BaseURL: ok.URL},
		{ID: "local", BaseURL: ok.URL},
	}})

	var body instancesResponse
	resp := getInstances(t, srv)
	if err := json.Unmarshal(resp.Body.Bytes(), &body); err != nil {
		t.Fatalf("decode: %v", err)
	}
	if len(body.Instances) != 2 {
		t.Fatalf("instances = %+v, want self + one ci", body.Instances)
	}
	if body.Instances[0].ID != "local" || body.Instances[1].ID != "ci" {
		t.Errorf("instances = %+v, want [local ci]", body.Instances)
	}
	for _, path := range []string{"/api/instances/bad/api/health", "/api/instances/local/api/health"} {
		rec := httptest.NewRecorder()
		srv.Handler().ServeHTTP(rec, httptest.NewRequest(http.MethodGet, path, nil))
		if rec.Code != http.StatusNotFound {
			t.Errorf("GET %s = %d, want 404", path, rec.Code)
		}
	}
}
