package server

// NOTE: Any changes to this file must be reflected in the corresponding SPECS.md or NOTES.md.

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"log/slog"
	"net"
	"net/http"
	"regexp"
	"sync"
	"time"
)

// Peer is one operator-configured remote wllr instance the origin server may
// proxy to. It is deliberately server-owned rather than a modules/client type:
// the server must not import the client (client SPECS: "never imported by any
// engine module").
type Peer struct {
	ID      string
	BaseURL string
	Token   string
}

// instanceView is one entry in the GET /api/instances response. protocol and
// last_seq are informational: they are always present (no omitempty on the
// ints) and are 0 for a down instance, so the contract is pointer-free. The
// page's selector renders only id and status; protocol/last_seq are exposed for
// other clients.
type instanceView struct {
	ID       string `json:"id"`
	Status   string `json:"status"`
	Error    string `json:"error,omitempty"`
	LastSeq  int64  `json:"last_seq"`
	Protocol int    `json:"protocol"`
	Self     bool   `json:"self"`
}

// statusOK and statusDown are the two values of an instance entry's status.
const (
	statusOK   = "ok"
	statusDown = "down"
)

// probeTimeout bounds one peer health probe and probeTTL caches its result, so a
// page polling /api/instances cannot hammer a down peer.
const (
	probeTimeout = 1500 * time.Millisecond
	probeTTL     = 5 * time.Second
)

// handleInstances reports this origin's own instance ID and one entry per
// configured peer, self first and peers in configuration order. It sits on
// s.mux, so the authenticate wrapper covers it exactly like every other route.
func (s *Server) handleInstances(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodGet {
		writeError(w, http.StatusMethodNotAllowed, "GET only")
		return
	}
	writeJSON(w, http.StatusOK, map[string]any{
		"self":      s.selfID,
		"instances": s.instanceViews(r.Context()),
	})
}

// instanceViews returns the self entry followed by one entry per peer, in
// configuration order. Peer probes run concurrently with per-probe timeouts, so
// one dead peer cannot delay the list beyond its own timeout. The self entry is
// read in-process; it is never probed and never proxied.
func (s *Server) instanceViews(ctx context.Context) []instanceView {
	views := make([]instanceView, len(s.peers)+1)
	views[0] = instanceView{
		ID: s.selfID, Self: true, Status: statusOK,
		Protocol: ProtocolVersion, LastSeq: s.hub.LastSeq(),
	}
	var wg sync.WaitGroup
	for i, peer := range s.peers {
		wg.Add(1)
		go func(i int, peer Peer) {
			defer wg.Done()
			views[i+1] = s.probe.view(ctx, peer)
		}(i, peer)
	}
	wg.Wait()
	return views
}

// peerIDPattern is the only id charset a peer may use. An id containing a slash,
// whitespace, or a query character can never match the /api/instances/{id}/api/
// mux wildcard, so accepting it would put an unroutable entry in the selector.
var peerIDPattern = regexp.MustCompile(`^[A-Za-z0-9._-]+$`)

// buildPeers filters the operator's peer table to the peers that can actually be
// reached and proxied: an empty id, an id outside the routable charset, an id
// that collides with self or an earlier peer, or a BaseURL that does not parse
// is dropped with a warning rather than failing startup or appearing in the
// browser's selector as a phantom instance. It returns the surviving peers and
// one reverse proxy per peer, keyed by id.
func buildPeers(configured []Peer, selfID string, logger *slog.Logger) ([]Peer, map[string]http.Handler) {
	peers := make([]Peer, 0, len(configured))
	proxies := make(map[string]http.Handler, len(configured))
	seen := map[string]bool{selfID: true}
	for _, p := range configured {
		if p.ID == "" {
			logger.Warn("wllr: dropping peer with empty id")
			continue
		}
		if !peerIDPattern.MatchString(p.ID) {
			logger.Warn("wllr: dropping peer with unusable id", "id", p.ID)
			continue
		}
		if seen[p.ID] {
			logger.Warn("wllr: dropping duplicate peer id", "id", p.ID)
			continue
		}
		proxy, err := newPeerProxy(p, logger)
		if err != nil {
			logger.Warn("wllr: dropping peer with unusable url", "id", p.ID, "err", err)
			continue
		}
		seen[p.ID] = true
		peers = append(peers, p)
		proxies[p.ID] = proxy
	}
	return peers, proxies
}

// health is one peer probe result: what the peer's /api/health answered, or why
// it did not. indeterminate marks a result that is not a real verdict about the
// peer (currently, the probe attempt was cancelled), which must never be cached.
type health struct {
	err           string
	protocol      int
	lastSeq       int64
	ok            bool
	indeterminate bool
}

// cachedHealth is a probe result with the time it was taken.
type cachedHealth struct {
	at time.Time
	h  health
}

// instanceProbe probes peer health behind a short TTL cache. The cache is the
// only mutable state the instances feature adds to the server.
type instanceProbe struct {
	client *http.Client
	cached map[string]cachedHealth
	mu     sync.Mutex
}

// newInstanceProbe builds a probe with a bounded HTTP client and an empty cache.
func newInstanceProbe() *instanceProbe {
	return &instanceProbe{
		client: &http.Client{Timeout: probeTimeout},
		cached: make(map[string]cachedHealth),
	}
}

// view returns the peer's instanceView, from the cache when it is fresh. The
// probe is deliberately decoupled from the caller's context: a client that aborts
// GET /api/instances must not cancel (and thus misclassify) a probe whose result
// the cache — and other requests — will rely on. Only a real verdict is cached; an
// indeterminate result (a cancelled attempt) is not.
func (p *instanceProbe) view(ctx context.Context, peer Peer) instanceView {
	h, ok := p.lookupHealth(peer.ID)
	if !ok {
		h = p.probeOnce(context.WithoutCancel(ctx), peer)
		if !h.indeterminate {
			p.storeHealth(peer.ID, h)
		}
	}
	v := instanceView{ID: peer.ID, Status: statusOK, Protocol: h.protocol, LastSeq: h.lastSeq}
	if !h.ok {
		v.Status = statusDown
		v.Error = h.err
	}
	return v
}

// lookupHealth returns a cached probe result when it is younger than probeTTL.
func (p *instanceProbe) lookupHealth(id string) (health, bool) {
	p.mu.Lock()
	defer p.mu.Unlock()
	c, ok := p.cached[id]
	if !ok || time.Since(c.at) > probeTTL {
		return health{}, false
	}
	return c.h, true
}

// storeHealth records a probe result under its peer id.
func (p *instanceProbe) storeHealth(id string, h health) {
	p.mu.Lock()
	defer p.mu.Unlock()
	p.cached[id] = cachedHealth{at: time.Now(), h: h}
}

// probeOnce performs one GET {peer}/api/health with a bounded timeout and the
// peer's token, and never returns an error — a failure is a "down" health, so
// one unreachable peer cannot fail the list. A probe whose attempt was cancelled
// is returned as indeterminate (not cached as down). A 200 is only healthy when
// the body is genuine wllr health (`ok:true` and a non-zero protocol), so a
// foreign service on a mistyped port is reported "malformed response" rather than
// "ok".
func (p *instanceProbe) probeOnce(ctx context.Context, peer Peer) health {
	ctx, cancel := context.WithTimeout(ctx, probeTimeout)
	defer cancel()
	healthURL, err := peerAPIURL(peer, "/api/health")
	if err != nil {
		return health{err: "unreachable"}
	}
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, healthURL, nil)
	if err != nil {
		return health{err: "unreachable"}
	}
	if peer.Token != "" {
		req.Header.Set("Authorization", "Bearer "+peer.Token)
	}
	resp, err := p.client.Do(req)
	if err != nil {
		return health{err: probeCause(err), indeterminate: errors.Is(err, context.Canceled)}
	}
	defer func() { _ = resp.Body.Close() }()
	if resp.StatusCode != http.StatusOK {
		return health{err: fmt.Sprintf("http %d", resp.StatusCode)}
	}
	var body struct {
		OK       bool  `json:"ok"`
		Protocol int   `json:"protocol"`
		LastSeq  int64 `json:"last_seq"`
	}
	if err := json.NewDecoder(io.LimitReader(resp.Body, 4096)).Decode(&body); err != nil {
		return health{err: "malformed response"}
	}
	if !body.OK || body.Protocol == 0 {
		return health{err: "malformed response"}
	}
	return health{ok: true, protocol: body.Protocol, lastSeq: body.LastSeq}
}

// probeCause maps a probe failure to a short, browser-safe cause. A raw net/http
// error embeds the peer's scheme, host, port, and path (for example
// `Get "http://127.0.0.1:8766/api/health": dial tcp 127.0.0.1:8766: connect:
// connection refused`), which must never reach the browser (invariant 12), so it
// is classified into a fixed vocabulary instead. Status codes are reported
// verbatim by the caller (`http <status>`): they reveal nothing about topology.
func probeCause(err error) string {
	var ne net.Error
	if errors.Is(err, context.DeadlineExceeded) || (errors.As(err, &ne) && ne.Timeout()) {
		return "timeout"
	}
	return "unreachable"
}
