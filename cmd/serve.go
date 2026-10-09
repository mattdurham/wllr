package main

// serve.go — `wllr serve` runs the same engine the TUI runs, with the renderer
// off and the HTTP/SSE API server on instead of the interactive program.
//
// It is deliberately not a separate, lighter-weight code path: main() performs
// the full wiring (provider, pool, extension host, built-in extensions, MCP
// bridge, native tools) before reaching this point, exactly as --exec does. The
// only difference is what drives the engine: a network API rather than a
// terminal.

import (
	"context"
	"errors"
	"fmt"
	"log/slog"
	"os"
	"os/signal"
	"syscall"

	"github.com/mattdurham/wllr/modules/agent"
	"github.com/mattdurham/wllr/modules/client"
	"github.com/mattdurham/wllr/modules/extension"
	"github.com/mattdurham/wllr/modules/harness"
	"github.com/mattdurham/wllr/modules/sdk"
	"github.com/mattdurham/wllr/modules/server"
)

// defaultServeAddr is the loopback address `wllr serve` binds when no address
// is given. Loopback by default so the API is not exposed on the network unless
// the operator asks for it; exposing it remote is an explicit choice (and
// should be paired with WLLR_API_TOKEN).
const defaultServeAddr = server.DefaultAddr

// serveArgs rewrites `wllr serve [addr] [flags...]` into the flag form
// `wllr --serve <addr> [flags...]` so the subcommand reuses main()'s existing
// flag parsing and full startup wiring rather than duplicating it.
func serveArgs(args []string) []string {
	addr := defaultServeAddr
	rest := args[2:]
	if len(rest) > 0 && !looksLikeFlag(rest[0]) {
		addr = rest[0]
		rest = rest[1:]
	}
	out := make([]string, 0, len(rest)+3)
	out = append(out, args[0], "--serve", addr)
	out = append(out, rest...)
	return out
}

// looksLikeFlag reports whether s is a flag rather than a positional address.
func looksLikeFlag(s string) bool {
	return len(s) > 0 && s[0] == '-'
}

// serveConfig is the operator-supplied multi-instance configuration for
// `wllr serve`: how this instance names itself and which peers the browser may
// switch to.
type serveConfig struct {
	SelfID string
	Peers  []server.Peer
}

// parseServeInstances turns -instance-id/-peer/-peers/-peer-token into the
// server's peer table, reusing the shared instance parser so `wllr serve` and
// `wllr-multiplex` declare instances identically.
//
// Unlike `wllr-multiplex`, where "no instances" is an error, a plain
// `wllr serve` must behave exactly as today: with no -peer and no -peers it
// returns an empty peer list (no error). The same is true when a peer source was
// given but yielded no entries (an empty `-peers` file): that is "no peers", not
// a startup failure, so an operator who generates an empty file can still start
// the server. Both cases are handled explicitly rather than by catching a generic
// parser error, so a genuine parser error is never swallowed.
func parseServeInstances(
	selfID string,
	peerFlags []string,
	peersFile, peerToken string,
) (serveConfig, error) {
	if selfID == "" {
		selfID = server.DefaultSelfID
	}
	if len(peerFlags) == 0 && peersFile == "" {
		return serveConfig{SelfID: selfID}, nil
	}
	// "peer"/"peers" are the flag names `wllr serve` actually exposes, so a
	// parser error names a flag the operator can pass.
	cfgs, err := client.BuildInstances(client.InstanceFlags(peerFlags), peersFile, peerToken, "peer", "peers")
	if err != nil {
		if errors.Is(err, client.ErrNoInstances) {
			slog.Warn("wllr: no peers configured; continuing without peers", "peers", peersFile)
			return serveConfig{SelfID: selfID}, nil
		}
		return serveConfig{}, err
	}
	return serveConfig{SelfID: selfID, Peers: peersFromConfigs(cfgs)}, nil
}

// peersFromConfigs maps the shared client.Config shape onto the server's
// server-owned Peer type, so modules/server never imports modules/client.
func peersFromConfigs(cfgs []client.Config) []server.Peer {
	peers := make([]server.Peer, 0, len(cfgs))
	for _, c := range cfgs {
		peers = append(peers, server.Peer{ID: c.ID, BaseURL: c.BaseURL, Token: c.Token})
	}
	return peers
}

// runServe starts the API server over the already-wired pool and blocks until
// the process receives SIGINT/SIGTERM (or ctx is cancelled), then shuts the
// HTTP server down gracefully. The caller's deferred cleanups (extension host,
// MCP bridge, logging) run when main returns.
//
// It installs the harness's *real* extension bridges backed by the server's
// renderer-neutral HubSink: extension UI (notifications, tool results,
// scene-graph patches) is published to the stream instead of dropped, and —
// because the agent bridge carries the sub-agent spawner — create_agent and the
// team tools work headlessly. Only the sink differs from the TUI's teaSink.
func runServe(
	ctx context.Context,
	pool *agent.AgentPool,
	h *extension.Host,
	m *harness.Model,
	addr string,
	sc serveConfig,
) error {
	token := os.Getenv("WLLR_API_TOKEN")
	srv := server.New(pool, server.Options{
		Addr:   addr,
		Token:  token,
		Peers:  sc.Peers,
		SelfID: sc.SelfID,
	})
	// Serve mode owns the single-slot context-usage dispatcher: the TUI harness
	// is not running, so nothing else claims it here.
	pool.SetContextUsageDispatcher(srv.HandleContextUsage)
	// Install the full extension bridges against the HTTP sink. A nil host is a
	// no-op, so serve still works when no extensions are loaded.
	harness.InstallHeadlessBridges(h, pool, agent.MainAgentID, serveLogFn(ctx), srv.HubSink())
	// Wire the root agent exactly as the TUI does. This is what gives the root
	// agent its tools and dispatches its extension events (before_agent_start,
	// token, steer) — without it a headless browser could chat but the agent had
	// no tools and the `agents` extension never built the chat transcript. The
	// sink is a no-op for the streaming frames because the server's per-agent
	// Bridge observers already publish Token/StreamDone/ToolCall for the root
	// agent; passing the HubSink here would publish every one of them twice.
	m.WireMainAgentCallbacks(&sdk.FuncSink{})
	// A turn's finalized assistant text must reach extensions, which a front end
	// with no tea loop has no other trigger for.
	srv.SetMessageEndNotifier(func(role, content string) {
		if err := m.DispatchMessageEnd(ctx, role, content); err != nil {
			slog.WarnContext(ctx, "api server: message end dispatch failed", "error", err)
		}
	})
	// Install the inbound half of the sink contract: a browser selection posts
	// to /api/ui/callback and this router dispatches it. Extension-owned
	// callbacks (agent focus/kill, transcript rebuild, WASM extension buttons)
	// reach the host. Core-owned ("__wllr:") callbacks reach m's core dispatch:
	// model / provider-model / thinking / OpenRouter-routing selections are
	// applied through the same classify+apply path the terminal uses, so
	// switching a model from a browser takes effect. The stateful setup flows
	// (auth, local-model, context-window, catalog) are TUI-only and answer 501.
	srv.SetCallbackRouter(harness.NewExtensionRouter(h, m.CoreCallbackFunc()))
	// Expose the same model list the terminal /model picker shows, so selecting a
	// model in the browser presents an identical choice set. ModelListFn is nil
	// when no provider catalog is wired, in which case the endpoint reports an
	// empty list rather than failing.
	if m.ModelListFn != nil {
		srv.SetModelLister(sdk.ModelListerFunc(m.ModelListFn))
	}
	// Expose the registered slash commands so a browser's autocomplete dropdown
	// and help offer exactly the command set the terminal does, and route any
	// command an extension registers through the sink into the same registry so
	// the list stays complete after wiring.
	srv.SetCommandLister(m.CommandLister())
	srv.HubSink().SetCommandRegistrar(m.RegisterCommand)

	// Run the session-boot extension dispatch that Init performs for the TUI.
	// A headless front end has no tea program, so Init never runs; without this
	// the bundled extensions never receive session_start / model_changed, and the
	// `agents` extension never creates the chat transcript area (the browser
	// would show only the fallback transcript). Done after the bridges are
	// installed so the extensions' ui_create_area/ui_patch calls reach the sink.
	if err := m.DispatchSessionStart(ctx); err != nil {
		slog.WarnContext(ctx, "api server: session start dispatch failed", "error", err)
	}
	if err := m.DispatchModelChanged(ctx); err != nil {
		slog.WarnContext(ctx, "api server: model changed dispatch failed", "error", err)
	}
	srv.Start()

	sigCtx, stop := signal.NotifyContext(ctx, os.Interrupt, syscall.SIGTERM)
	defer stop()

	if err := srv.Serve(sigCtx); err != nil {
		return fmt.Errorf("api server: %w", err)
	}
	return nil
}

// serveLogFn adapts the harness's numeric-level log callback to slog, matching
// the adapter the interactive path installs via Model.SetLogFn.
func serveLogFn(ctx context.Context) func(int, string) {
	return func(level int, msg string) {
		lvl := []slog.Level{slog.LevelDebug, slog.LevelInfo, slog.LevelWarn, slog.LevelError}
		l := slog.LevelError
		if level >= 0 && level < len(lvl) {
			l = lvl[level]
		}
		slog.Log(ctx, l, msg)
	}
}
