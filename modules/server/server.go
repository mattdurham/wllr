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
	"strconv"
	"strings"
	"time"

	"github.com/mattdurham/wllr/modules/agent"
	"github.com/mattdurham/wllr/modules/sdk"
)

// DefaultBacklog is the default replay buffer size, roughly two minutes of a
// busy turn's token events.
const DefaultBacklog = 4096

// DefaultAddr is the default loopback listen address.
const DefaultAddr = "127.0.0.1:8765"

// agentIDKey is the JSON field name for the agent every response concerns, used
// by the history/submit/cancel/inbox handlers so the wire spelling is defined
// once.
const agentIDKey = "agent_id"

// Server exposes the engine over HTTP: a REST surface for history, submission,
// cancellation, and agent/inbox inspection, plus a Server-Sent Events stream
// that fans out the engine's callbacks via the Hub.
//
// A Server holds a read-mostly view of an *agent.AgentPool. It owns a Hub and a
// Bridge; it never mutates engine state except through the pool's own public
// methods (Send, Cancel, inbox edits), so it cannot violate the pool's
// invariants.
type Server struct {
	router   sdk.UICallbackRouter
	models   sdk.ModelLister
	commands sdk.CommandLister
	pool     *agent.AgentPool
	hub      *Hub
	bridge   *Bridge
	sink     *HubSink
	mux      *http.ServeMux
	logger   *slog.Logger

	// messageEnd is invoked when a turn's assistant text is finalized. See
	// SetMessageEndNotifier.
	messageEnd func(role, content string)
	token      string
	addr       string
	mainID     string
}

// New builds a Server over pool. It does not start serving or install observers;
// call Start and Serve.
func New(pool *agent.AgentPool, opts Options) *Server {
	logger := opts.Logger
	if logger == nil {
		logger = slog.Default()
	}
	mainID := opts.MainID
	if mainID == "" {
		mainID = agent.MainAgentID
	}
	backlog := opts.Backlog
	if backlog == 0 {
		backlog = DefaultBacklog
	}
	addr := opts.Addr
	if addr == "" {
		addr = DefaultAddr
	}
	hub := NewHub(backlog)
	s := &Server{
		pool:   pool,
		hub:    hub,
		bridge: NewBridge(pool, hub, mainID),
		sink:   NewHubSink(pool, hub, mainID),
		mux:    http.NewServeMux(),
		token:  opts.Token,
		addr:   addr,
		mainID: mainID,
		logger: logger,
	}
	s.routes()
	return s
}

// Hub exposes the event hub for tests and for callers that want to publish
// their own events (for example an extension bridge added later).
func (s *Server) Hub() *Hub { return s.hub }

// HubSink returns the renderer-neutral front-end sink for serve mode. Serve
// mode installs the harness's extension bridges (agent, team, UI) with this
// sink, so extension UI — notifications, tool results, and scene-graph patches
// — reaches the stream instead of being dropped, and sub-agents can be spawned.
func (s *Server) HubSink() *HubSink { return s.sink }

// SetCallbackRouter installs the router that POST /api/ui/callback dispatches
// through. Serve mode installs a harness.ExtensionRouter so a browser picking a
// model, focusing an agent, or answering a text prompt reaches the same engine
// handler the TUI would have invoked. Without a router the endpoint returns 501.
func (s *Server) SetCallbackRouter(r sdk.UICallbackRouter) { s.router = r }

// SetModelLister installs the source of selectable models for GET /api/models.
// Serve mode passes the harness's ModelListFn, so the browser presents exactly
// the list the terminal's /model picker shows. A nil lister makes the endpoint
// answer with an empty list rather than an error.
func (s *Server) SetModelLister(l sdk.ModelLister) { s.models = l }

// SetCommandLister installs the source of registered slash commands for
// GET /api/commands. Serve mode passes the harness's registry, so a browser's
// autocomplete dropdown and help offer exactly the commands the terminal does.
// A nil lister makes the endpoint answer with an empty list.
func (s *Server) SetCommandLister(l sdk.CommandLister) { s.commands = l }

// Start installs the Bridge observers, attaching per-agent stream hooks.
func (s *Server) Start() {
	s.bridge.SetMessageEndNotifier(s.messageEnd)
	s.bridge.Start()
}

// SetMessageEndNotifier installs the callback invoked when a turn's assistant
// text is finalized, with the role and the accumulated content. Serve mode
// points it at the harness so EventMessageEnd is dispatched to extensions —
// the bundled `agents` extension renders the finalized (markdown) assistant
// bubble only on that event, and `history` records the response. A front end
// with no tea loop must fire it itself; the TUI fires it from its update loop.
func (s *Server) SetMessageEndNotifier(fn func(role, content string)) { s.messageEnd = fn }

// HandleContextUsage adapts the Bridge's context-usage handler to the exact
// signature of agent.AgentPool.SetContextUsageDispatcher. Serve mode installs
// it so the main agent's context window usage and compaction notices reach
// connected clients. Interactive mode leaves the dispatcher to the TUI harness.
func (s *Server) HandleContextUsage(cu sdk.ContextUsage, notice *agent.CompactionNotice, compactions int) {
	s.bridge.HandleContextUsage(cu, notice, compactions)
}

// Handler returns the HTTP handler, wrapped with auth and logging. It is
// exported so tests can drive the API without binding a socket.
func (s *Server) Handler() http.Handler {
	return s.logRequests(s.authenticate(s.mux))
}

// Serve listens on the configured address and serves until ctx is cancelled,
// then shuts down gracefully. It returns nil on a clean shutdown.
func (s *Server) Serve(ctx context.Context) error {
	srv := &http.Server{
		Addr:              s.addr,
		Handler:           s.Handler(),
		ReadHeaderTimeout: 10 * time.Second,
		// No WriteTimeout: the SSE stream is long-lived and intentionally idle
		// between events (heartbeats keep the connection alive).
		BaseContext: func(net.Listener) context.Context { return ctx },
	}
	errCh := make(chan error, 1)
	go func() {
		s.logger.Info("wllr: api server listening", "addr", s.addr, "auth", s.token != "")
		if err := srv.ListenAndServe(); err != nil && !errors.Is(err, http.ErrServerClosed) {
			errCh <- err
			return
		}
		errCh <- nil
	}()
	select {
	case err := <-errCh:
		return err
	case <-ctx.Done():
		shutdownCtx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
		defer cancel()
		return srv.Shutdown(shutdownCtx)
	}
}

// routes registers all API handlers on the mux.
func (s *Server) routes() {
	s.mux.HandleFunc("/api/health", s.handleHealth)
	s.mux.HandleFunc("/api/agents", s.handleAgents)
	s.mux.HandleFunc("/api/history", s.handleHistory)
	s.mux.HandleFunc("/api/context", s.handleContext)
	s.mux.HandleFunc("/api/submit", s.handleSubmit)
	s.mux.HandleFunc("/api/cancel", s.handleCancel)
	s.mux.HandleFunc("/api/inbox", s.handleInbox)
	s.mux.HandleFunc("/api/inbox/clear", s.handleInboxClear)
	s.mux.HandleFunc("/api/inbox/edit", s.handleInboxEdit)
	s.mux.HandleFunc("/api/stream", s.handleStream)
	s.mux.HandleFunc("/api/scene", s.handleScene)
	s.mux.HandleFunc("/api/models", s.handleModels)
	s.mux.HandleFunc("/api/commands", s.handleCommands)
	s.mux.HandleFunc("/api/ui/callback", s.handleUICallback)
	s.mux.HandleFunc("/", s.handleClient)
}

// modelView is one entry in the GET /api/models response. Key is the
// provider-qualified picker key (sdk.ModelPickerKey) the client echoes back in a
// selection, so the client never has to know the key encoding — the same reason
// the separator lives in one place.
type modelView struct {
	ID       string   `json:"id"`
	Name     string   `json:"name,omitempty"`
	Sublabel string   `json:"sublabel,omitempty"`
	Provider string   `json:"provider,omitempty"`
	Key      string   `json:"key"`
	Tiers    []string `json:"tiers,omitempty"`
	Active   bool     `json:"active,omitempty"`
}

// handleModels is the read half of model selection: it returns the models a
// user can switch to and the callback id to switch under. Serve mode installs
// the harness's ModelListFn, so this is exactly the terminal /model list. The
// client renders it and POSTs the chosen Key to /api/ui/callback, which routes
// through the same core dispatch the terminal picker uses.
func (s *Server) handleModels(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodGet {
		writeError(w, http.StatusMethodNotAllowed, "GET only")
		return
	}
	provider, model := "", ""
	if s.pool != nil {
		provider = s.pool.ProviderName()
		if main := s.pool.Get(s.mainID); main != nil {
			model = main.ModelName()
		}
	}
	out := []modelView{}
	if s.models != nil {
		for _, c := range s.models.ModelChoices() {
			out = append(out, modelView{
				ID:       c.ID,
				Name:     c.Name,
				Sublabel: c.Sublabel,
				Provider: c.Provider,
				Tiers:    c.Tiers,
				Active:   c.Active,
				Key:      sdk.ModelPickerKey(c.Provider, c.ID),
			})
		}
	}
	writeJSON(w, http.StatusOK, map[string]any{
		"models":   out,
		"callback": sdk.ModelPickerCallback,
		"current":  map[string]string{"provider": provider, "model": model},
	})
}

// handleCommands returns the registered slash commands so a browser can offer
// the same autocomplete dropdown and help the terminal does. Instant flags
// whether a command runs immediately rather than being queued to an extension.
func (s *Server) handleCommands(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodGet {
		writeError(w, http.StatusMethodNotAllowed, "GET only")
		return
	}
	out := []sdk.CommandInfo{}
	if s.commands != nil {
		out = append(out, s.commands.ListCommands()...)
	}
	writeJSON(w, http.StatusOK, map[string]any{
		"commands": out,
		"prefix":   "/",
	})
}

// handleScene returns every scene area with its rendered HTML, in creation
// order. A client fetches this once on connect to seed its view, then keeps it
// current from ui_area_html / ui_text_append / ui_area_remove stream frames.
func (s *Server) handleScene(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodGet {
		writeError(w, http.StatusMethodNotAllowed, "GET only")
		return
	}
	writeJSON(w, http.StatusOK, map[string]any{
		"protocol": ProtocolVersion,
		"areas":    s.sink.SceneModel().Snapshot(),
	})
}

// uiCallbackRequest is the POST /api/ui/callback body. A picker sends the
// selected item id as Value; a text input sends the entered string; an agent
// tree action sends the agent id. Args, when present, overrides the single
// Value (for callbacks that take more than one argument).
type uiCallbackRequest struct {
	Callback string   `json:"callback"`
	Value    string   `json:"value"`
	Args     []string `json:"args,omitempty"`
}

// handleUICallback routes a front-end selection back into the engine. It is the
// inbound half of the UISink contract: the stream carries a picker/text-input
// frame with a Callback id, and the client posts that id (with the chosen
// value) here. The Server stays transport-only — the installed router owns the
// core/extension split — so this handler cannot reach engine state the pool's
// own methods do not expose.
func (s *Server) handleUICallback(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodPost {
		writeError(w, http.StatusMethodNotAllowed, "POST only")
		return
	}
	if s.router == nil {
		writeError(w, http.StatusNotImplemented, "no UI callback router configured")
		return
	}
	var req uiCallbackRequest
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		writeError(w, http.StatusBadRequest, "invalid JSON body")
		return
	}
	if strings.TrimSpace(req.Callback) == "" {
		writeError(w, http.StatusBadRequest, "callback is required")
		return
	}
	args := req.Args
	if len(args) == 0 {
		args = []string{req.Value}
	}
	err := s.router.RouteCallback(r.Context(), req.Callback, args)
	switch {
	case err == nil:
		writeJSON(w, http.StatusOK, map[string]any{"callback": req.Callback, "routed": true})
	case errors.Is(err, sdk.ErrNoCoreHandler), errors.Is(err, sdk.ErrNoExtensionHost):
		// The callback is well-formed but this front end does not handle its
		// class (for example a core model switch with no core handler). Answer
		// "not implemented" rather than 500 so a client can fall back.
		writeError(w, http.StatusNotImplemented, err.Error())
	default:
		writeError(w, http.StatusInternalServerError, err.Error())
	}
}

// handleClient serves the self-contained HTML front end. It is a single page
// that talks to the same REST surface and event stream as any other client; it
// is the "HTML" half of the renderer-neutral sink. Requests for any other path
// under "/" fall through to a 404 so API routes are unaffected.
//
// The page's script checks the server protocol version against the value
// substituted here, so the expected version has one source of truth (wire.go).
func (s *Server) handleClient(w http.ResponseWriter, r *http.Request) {
	if r.URL.Path != "/" {
		writeError(w, http.StatusNotFound, "not found")
		return
	}
	w.Header().Set("Content-Type", "text/html; charset=utf-8")
	page := strings.ReplaceAll(clientHTML, "__PROTOCOL__", strconv.Itoa(ProtocolVersion))
	_, _ = io.WriteString(w, page)
}

// authenticate enforces the bearer token when one is configured.
func (s *Server) authenticate(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if s.token == "" {
			next.ServeHTTP(w, r)
			return
		}
		tok := ""
		if h := r.Header.Get("Authorization"); strings.HasPrefix(h, "Bearer ") {
			tok = strings.TrimPrefix(h, "Bearer ")
		} else if q := r.URL.Query().Get("token"); q != "" {
			tok = q
		}
		if tok != s.token {
			writeError(w, http.StatusUnauthorized, "unauthorized")
			return
		}
		next.ServeHTTP(w, r)
	})
}

// logRequests logs method, path, and status at debug level. Status capture is
// best-effort (a handler that hijacks the connection reports 200).
func (s *Server) logRequests(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		start := time.Now()
		rec := &statusRecorder{ResponseWriter: w, status: http.StatusOK}
		next.ServeHTTP(rec, r)
		s.logger.Debug("wllr: api request",
			"method", r.Method,
			"path", r.URL.Path,
			"status", rec.status,
			"duration_ms", time.Since(start).Milliseconds(),
		)
	})
}

// statusRecorder captures the response status for logging. It must forward the
// optional interfaces an SSE/streaming handler depends on: embedding only the
// http.ResponseWriter interface would hide them, so a handler wrapping the
// recorder could not assert http.Flusher (which broke /api/stream).
type statusRecorder struct {
	http.ResponseWriter
	status int
}

func (r *statusRecorder) WriteHeader(code int) {
	r.status = code
	r.ResponseWriter.WriteHeader(code)
}

// Flush forwards to the underlying writer when it supports flushing, so the
// events stream is pushed to the client immediately.
func (r *statusRecorder) Flush() {
	if f, ok := r.ResponseWriter.(http.Flusher); ok {
		f.Flush()
	}
}

// Unwrap exposes the underlying writer to http.ResponseController so future
// streaming features (hijack for WebSocket upgrades) keep working through the
// logging wrapper.
func (r *statusRecorder) Unwrap() http.ResponseWriter { return r.ResponseWriter }

// requirePool rejects a request with 503 when the server has no agent pool
// configured, so a handler never dereferences a nil pool.
func (s *Server) requirePool(w http.ResponseWriter) bool {
	if s.pool == nil {
		writeError(w, http.StatusServiceUnavailable, "no agent pool")
		return false
	}
	return true
}

// resolveAgent returns the requested agent ID, defaulting to the root agent
// when the query/body omits one. It errors when the named agent is unknown.
func (s *Server) resolveAgent(id string) (string, error) {
	if id == "" {
		return s.mainID, nil
	}
	if s.pool == nil || s.pool.Get(id) == nil {
		return "", fmt.Errorf("agent %q not found", id)
	}
	return id, nil
}

func (s *Server) handleHealth(w http.ResponseWriter, _ *http.Request) {
	subs, drops := s.hub.Stats()
	writeJSON(w, http.StatusOK, map[string]any{
		"ok":          true,
		"protocol":    ProtocolVersion,
		"last_seq":    s.hub.LastSeq(),
		"subscribers": subs,
		"drops":       drops,
	})
}

// agentView is one entry in the agent-tree response.
type agentView struct {
	ID          string `json:"id"`
	Name        string `json:"name,omitempty"`
	ActiveTool  string `json:"active_tool,omitempty"`
	LastTool    string `json:"last_tool,omitempty"`
	Inbox       int    `json:"inbox"`
	HistoryLen  int    `json:"history_len"`
	Compactions int    `json:"compactions"`
	Running     bool   `json:"running"`
	Main        bool   `json:"main"`
}

func (s *Server) handleAgents(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodGet {
		writeError(w, http.StatusMethodNotAllowed, "GET only")
		return
	}
	if s.pool == nil {
		writeJSON(w, http.StatusOK, map[string]any{"agents": []agentView{}})
		return
	}
	ids := s.pool.ListAgents()
	out := make([]agentView, 0, len(ids))
	for _, id := range ids {
		a := s.pool.Get(id)
		if a == nil {
			continue
		}
		activity := a.Activity()
		out = append(out, agentView{
			ID:          id,
			Name:        a.Name(),
			Running:     a.IsRunning(),
			Inbox:       a.InboxLen(),
			HistoryLen:  len(a.History()),
			ActiveTool:  activity.ActiveToolName,
			LastTool:    activity.LastToolName,
			Main:        id == s.mainID,
			Compactions: a.CompactionCount(),
		})
	}
	writeJSON(w, http.StatusOK, map[string]any{"agents": out})
}

func (s *Server) handleHistory(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodGet {
		writeError(w, http.StatusMethodNotAllowed, "GET only")
		return
	}
	if !s.requirePool(w) {
		return
	}
	q := r.URL.Query()
	id, err := s.resolveAgent(q.Get("agent"))
	if err != nil {
		writeError(w, http.StatusNotFound, err.Error())
		return
	}
	a := s.pool.Get(id)
	if a == nil {
		writeError(w, http.StatusNotFound, resourceMissing(id))
		return
	}
	history := a.History()
	total := len(history)
	offset := parseIntDefault(q.Get("offset"), 0)
	limit := parseIntDefault(q.Get("limit"), 0)
	if offset < 0 {
		offset = 0
	}
	if offset > total {
		offset = total
	}
	history = history[offset:]
	if limit > 0 && limit < len(history) {
		history = history[:limit]
	}
	// Each message carries a pre-rendered HTML body (mdhtml.go) alongside its
	// raw content, so a browser shows markdown exactly as the terminal does
	// without reimplementing the renderer. The raw Content is still returned for
	// non-browser clients and for copying.
	views := make([]messageView, 0, len(history))
	for _, m := range history {
		views = append(views, messageView{
			ID:      m.ID,
			Role:    m.Role,
			Content: m.Content,
			Type:    m.Type,
			HTML:    MessageHTML(m.Role, m.Content),
		})
	}
	writeJSON(w, http.StatusOK, map[string]any{
		agentIDKey: id,
		"total":    total,
		"offset":   offset,
		"messages": views,
	})
}

// messageView is one entry in the history response: the stored message plus a
// rendered HTML body, so a client can display markdown without a renderer of
// its own.
type messageView struct {
	ID      string          `json:"id,omitempty"`
	Role    sdk.Role        `json:"role"`
	Content string          `json:"content"`
	Type    sdk.MessageType `json:"type,omitempty"`
	HTML    string          `json:"html,omitempty"`
}

// submitRequest is the POST /api/submit body.
type submitRequest struct {
	Agent   string `json:"agent"`
	Content string `json:"content"`
}

func (s *Server) handleSubmit(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodPost {
		writeError(w, http.StatusMethodNotAllowed, "POST only")
		return
	}
	if !s.requirePool(w) {
		return
	}
	var req submitRequest
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		writeError(w, http.StatusBadRequest, "invalid JSON body")
		return
	}
	if strings.TrimSpace(req.Content) == "" {
		writeError(w, http.StatusBadRequest, "content is required")
		return
	}
	id, err := s.resolveAgent(req.Agent)
	if err != nil {
		writeError(w, http.StatusNotFound, err.Error())
		return
	}
	if err := s.pool.Send(id, req.Content); err != nil {
		writeError(w, http.StatusInternalServerError, err.Error())
		return
	}
	writeJSON(w, http.StatusAccepted, map[string]any{agentIDKey: id, "accepted": true})
}

// cancelRequest is the POST /api/cancel body.
type cancelRequest struct {
	Agent string `json:"agent"`
}

func (s *Server) handleCancel(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodPost {
		writeError(w, http.StatusMethodNotAllowed, "POST only")
		return
	}
	if !s.requirePool(w) {
		return
	}
	var req cancelRequest
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		writeError(w, http.StatusBadRequest, "invalid JSON body")
		return
	}
	id, err := s.resolveAgent(req.Agent)
	if err != nil {
		writeError(w, http.StatusNotFound, err.Error())
		return
	}
	if err := s.pool.Cancel(id); err != nil {
		writeError(w, http.StatusInternalServerError, err.Error())
		return
	}
	writeJSON(w, http.StatusOK, map[string]any{agentIDKey: id, "cancelled": true})
}

func (s *Server) handleInbox(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodGet {
		writeError(w, http.StatusMethodNotAllowed, "GET only")
		return
	}
	if !s.requirePool(w) {
		return
	}
	id, err := s.resolveAgent(r.URL.Query().Get("agent"))
	if err != nil {
		writeError(w, http.StatusNotFound, err.Error())
		return
	}
	msgs, err := s.pool.SnapshotInbox(id)
	if err != nil {
		writeError(w, http.StatusNotFound, resourceMissing(id))
		return
	}
	writeJSON(w, http.StatusOK, map[string]any{agentIDKey: id, "messages": msgs})
}

func (s *Server) handleInboxClear(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodPost {
		writeError(w, http.StatusMethodNotAllowed, "POST only")
		return
	}
	if !s.requirePool(w) {
		return
	}
	var req cancelRequest
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		writeError(w, http.StatusBadRequest, "invalid JSON body")
		return
	}
	id, err := s.resolveAgent(req.Agent)
	if err != nil {
		writeError(w, http.StatusNotFound, err.Error())
		return
	}
	n, err := s.pool.ClearInbox(id)
	if err != nil {
		writeError(w, http.StatusNotFound, resourceMissing(id))
		return
	}
	writeJSON(w, http.StatusOK, map[string]any{agentIDKey: id, "cleared": n})
}

// inboxEditRequest is the POST /api/inbox/edit body. Exactly one of Index or
// MessageID selects the target, matching the pool's edit semantics.
type inboxEditRequest struct {
	Agent     string `json:"agent"`
	Index     *int   `json:"index"`
	MessageID string `json:"message_id"`
	Content   string `json:"content"`
}

func (s *Server) handleInboxEdit(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodPost {
		writeError(w, http.StatusMethodNotAllowed, "POST only")
		return
	}
	if !s.requirePool(w) {
		return
	}
	var req inboxEditRequest
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		writeError(w, http.StatusBadRequest, "invalid JSON body")
		return
	}
	id, err := s.resolveAgent(req.Agent)
	if err != nil {
		writeError(w, http.StatusNotFound, err.Error())
		return
	}
	byIndex := -1
	if req.Index != nil {
		byIndex = *req.Index
	}
	if err := s.pool.EditInboxMessage(id, byIndex, req.MessageID, req.Content); err != nil {
		writeError(w, http.StatusBadRequest, err.Error())
		return
	}
	writeJSON(w, http.StatusOK, map[string]any{agentIDKey: id, "edited": true})
}

// handleStream serves the Server-Sent Events stream. Each event is a JSON
// Event frame on a `data:` line. A `since` query parameter replays retained
// events after that sequence number, so a reconnecting client resumes without
// gaps (subject to the backlog limit).
func (s *Server) handleStream(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodGet {
		writeError(w, http.StatusMethodNotAllowed, "GET only")
		return
	}
	flusher, ok := w.(http.Flusher)
	if !ok {
		writeError(w, http.StatusInternalServerError, "streaming unsupported")
		return
	}
	since := int64(parseIntDefault(r.URL.Query().Get("since"), 0))

	w.Header().Set("Content-Type", "text/event-stream")
	w.Header().Set("Cache-Control", "no-cache")
	w.Header().Set("Connection", "keep-alive")
	w.Header().Set("X-Accel-Buffering", "no")
	w.WriteHeader(http.StatusOK)

	events, replay, cancel := s.hub.Subscribe(since)
	defer cancel()

	if err := writeSSE(w, Event{
		Kind:     KindReady,
		Protocol: ProtocolVersion,
		Seq:      s.hub.LastSeq(),
	}); err != nil {
		return
	}
	for _, e := range replay {
		if err := writeSSE(w, e); err != nil {
			return
		}
	}
	flusher.Flush()

	heartbeat := time.NewTicker(15 * time.Second)
	defer heartbeat.Stop()
	ctx := r.Context()
	for {
		select {
		case <-ctx.Done():
			return
		case e, ok := <-events:
			if !ok {
				return
			}
			if err := writeSSE(w, e); err != nil {
				return
			}
			flusher.Flush()
		case <-heartbeat.C:
			if _, err := fmt.Fprint(w, ": keep-alive\n\n"); err != nil {
				return
			}
			flusher.Flush()
		}
	}
}

// writeSSE encodes one Event as a single SSE frame.
func writeSSE(w http.ResponseWriter, e Event) error {
	data, err := json.Marshal(e)
	if err != nil {
		return err
	}
	_, err = fmt.Fprintf(w, "data: %s\n\n", data)
	return err
}

// writeJSON writes v as JSON with the given status.
func writeJSON(w http.ResponseWriter, status int, v any) {
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(status)
	_ = json.NewEncoder(w).Encode(v)
}

// writeError writes a JSON error body with the given status.
func writeError(w http.ResponseWriter, status int, msg string) {
	writeJSON(w, status, map[string]any{"error": msg})
}

// resourceMissing renders a not-found message for a resolved-but-vanished agent.
func resourceMissing(id string) string {
	return fmt.Sprintf("agent %q not found", id)
}

// parseIntDefault parses s as an int, returning def when s is empty or invalid.
func parseIntDefault(s string, def int) int {
	if s == "" {
		return def
	}
	n, err := strconv.Atoi(s)
	if err != nil {
		return def
	}
	return n
}
