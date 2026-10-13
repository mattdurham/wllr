package server

// NOTE: Any changes to this file must be reflected in the corresponding SPECS.md or NOTES.md.

import (
	"fmt"
	"log/slog"
	"net"
	"net/http"
	"net/http/httputil"
	"net/url"
	"strings"
	"time"
)

// peerProxyPrefix is the path prefix under which a peer's API is proxied. The
// instance id is a mux wildcard, so a path outside /api/instances/{id}/api/ is
// never forwarded.
const peerProxyPrefix = "/api/instances/"

// peerTransport is the shared transport for every peer proxy. It is explicit
// rather than http.DefaultTransport because the default has no
// ResponseHeaderTimeout: a peer that completes the TCP handshake but never
// writes response headers (a stopped process, a half-open NAT/firewall flow, a
// TLS-to-plain-HTTP port mismatch) would otherwise block the proxied request —
// including the page's EventSource, which never errors so the page cannot notice
// — until the inbound client gives up. The header budget is probeTimeout, the
// same bound the health probe uses. ResponseHeaderTimeout is safe for the SSE
// stream because a stream's headers flush immediately and the body is governed
// by the other knobs.
var peerTransport = &http.Transport{
	Proxy: http.ProxyFromEnvironment,
	DialContext: (&net.Dialer{
		Timeout:   probeTimeout,
		KeepAlive: 30 * time.Second,
	}).DialContext,
	ResponseHeaderTimeout: probeTimeout,
	// TLSHandshakeTimeout is set for the same reason: ResponseHeaderTimeout is
	// armed only after the connection (including the TLS handshake) is
	// established, so without it an https peer that completes the TCP connect but
	// stalls mid-handshake would be unbounded.
	TLSHandshakeTimeout: probeTimeout,
	MaxIdleConns:        100,
	IdleConnTimeout:     90 * time.Second,
}

// peerTarget parses and validates a peer's BaseURL. It is shared by the reverse
// proxy and the health probe so both agree on what a peer's base is (and on
// which BaseURLs are usable).
func peerTarget(peer Peer) (*url.URL, error) {
	target, err := url.Parse(peer.BaseURL)
	if err != nil {
		return nil, fmt.Errorf("peer %q: %w", peer.ID, err)
	}
	if target.Scheme == "" || target.Host == "" {
		return nil, fmt.Errorf("peer %q: url %q has no scheme or host", peer.ID, peer.BaseURL)
	}
	return target, nil
}

// peerJoinPath joins a peer's base path with a request API path, mirroring the
// stdlib httputil.ReverseProxy's joinURLPath (what the proxy's SetURL performs)
// for the subset the probe needs. The probe resolves a BaseURL that carries a
// path (for example http://host/wllr) the same way the proxy does.
func peerJoinPath(base, reqPath string) string {
	if base == "" {
		return reqPath
	}
	if reqPath == "" {
		return base
	}
	baseSlash := strings.HasSuffix(base, "/")
	reqSlash := strings.HasPrefix(reqPath, "/")
	switch {
	case baseSlash && reqSlash:
		return base + reqPath[1:]
	case !baseSlash && !reqSlash:
		return base + "/" + reqPath
	default:
		return base + reqPath
	}
}

// peerAPIURL resolves an /api path against a peer's BaseURL using the same path
// join the proxy's SetURL performs, so the health probe never disagrees with the
// proxy for a BaseURL that carries a path or query (L2).
func peerAPIURL(peer Peer, apiPath string) (string, error) {
	target, err := peerTarget(peer)
	if err != nil {
		return "", err
	}
	u := *target
	u.Path = peerJoinPath(target.Path, apiPath)
	u.RawPath = ""
	return u.String(), nil
}

// newPeerProxy builds the reverse proxy for one peer. It strips the
// /api/instances/{id} prefix, forwards the peer's /api surface verbatim
// (including the SSE stream), and injects the peer's bearer token server-side so
// it never reaches the browser. A proxy failure is logged with the peer id (the
// browser body stays generic) so an operator can see why a peer is unreachable.
func newPeerProxy(peer Peer, logger *slog.Logger) (http.Handler, error) {
	target, err := peerTarget(peer)
	if err != nil {
		return nil, err
	}
	strip := peerProxyPrefix + peer.ID
	return &httputil.ReverseProxy{
		Transport: peerTransport,
		Rewrite: func(pr *httputil.ProxyRequest) {
			// Strip the /api/instances/{id} prefix from the inbound path *before*
			// the join: SetURL joins target.Path with the outbound path, so trimming
			// afterwards would leave the prefix embedded whenever the peer's BaseURL
			// carries a path (http://host/wllr) — the peer would 404 while the probe
			// (peerAPIURL) reported it healthy (L2).
			pr.Out.URL.Path = strings.TrimPrefix(pr.In.URL.Path, strip)
			// The path changed, so a stale escaped form must not survive.
			pr.Out.URL.RawPath = ""
			pr.SetURL(target)
			// The origin's own credentials must never travel to a peer, in
			// either form the origin accepts (Authorization header, ?token=), and
			// the peer's own token is injected here instead.
			pr.Out.Header.Del("Authorization")
			q := pr.Out.URL.Query()
			q.Del("token")
			pr.Out.URL.RawQuery = q.Encode()
			if peer.Token != "" {
				pr.Out.Header.Set("Authorization", "Bearer "+peer.Token)
			}
		},
		ModifyResponse: func(resp *http.Response) error {
			// A peer response is delivered under the *origin's* origin, so it must
			// not carry cross-origin state: relativize Location (invariant 12) and
			// drop Set-Cookie (nothing is cookie-based, and a planted cookie would
			// become a privilege boundary the origin never set).
			resp.Header.Del("Set-Cookie")
			relativizeLocation(resp)
			return nil
		},
		ErrorHandler: func(w http.ResponseWriter, _ *http.Request, err error) {
			logger.Warn("wllr: peer proxy failed", "id", peer.ID, "err", err)
			writeError(w, http.StatusBadGateway, "instance unreachable")
		},
	}, nil
}

// relativizeLocation reduces a 3xx Location to a same-origin absolute path so a
// peer can never hand the browser a cross-origin target (invariant 12). Only a
// 3xx carries a meaningful Location; every other response is untouched.
//
// The test is on the parsed scheme/host/opaque, *not* url.URL.IsAbs: a
// protocol-relative `//host/x` parses with Scheme=="" so IsAbs() is false, yet a
// browser resolves it against the origin's scheme and would follow it off-origin.
// Backslash variants (`/\host/x`) are likewise refused. Anything with an
// authority or a non-path scheme is collapsed to its path+query (or "/"); a
// Location that is already a plain relative path is left alone.
func relativizeLocation(resp *http.Response) {
	if resp.StatusCode < 300 || resp.StatusCode >= 400 {
		return
	}
	loc := resp.Header.Get("Location")
	if loc == "" {
		return
	}
	u, err := url.Parse(loc)
	if err != nil {
		return
	}
	if u.Scheme == "" && u.Host == "" && u.Opaque == "" &&
		!strings.HasPrefix(loc, "//") && !strings.ContainsRune(loc, '\\') {
		// Already a plain relative path: leave as-is.
		return
	}
	rel := u.EscapedPath()
	if rel == "" {
		rel = "/"
	} else if !strings.HasPrefix(rel, "/") {
		rel = "/" + rel
	}
	if u.RawQuery != "" {
		rel += "?" + u.RawQuery
	}
	resp.Header.Set("Location", rel)
}

// handlePeerProxy forwards a peer's API path to that peer. Only a known id is
// proxied, so this is not an open proxy: the targets come only from operator
// config and a path outside the peer's /api surface never matches the route.
func (s *Server) handlePeerProxy(w http.ResponseWriter, r *http.Request) {
	proxy, ok := s.proxies[r.PathValue("id")]
	if !ok {
		writeError(w, http.StatusNotFound, "unknown instance")
		return
	}
	proxy.ServeHTTP(w, r)
}
