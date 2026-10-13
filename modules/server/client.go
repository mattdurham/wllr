package server

// NOTE: Any changes to this file must be reflected in the corresponding SPECS.md or NOTES.md.

// clientHTML is the self-contained browser front end served at GET /. It is a
// single page with no external assets. It talks to exactly the same REST
// surface and event stream as any other API client — it holds no privileged
// access — which is what demonstrates the point of the renderer-neutral sink:
// the TUI and this page drive one engine through one contract.
//
// A page may switch between instances: GET /api/instances lists this origin and
// its operator-configured peers, and every per-instance call is prefixed with
// the selected instance via api(). The local instance keeps the plain relative
// path and is served in-process; a peer goes through the origin server's reverse
// proxy, which injects the peer token server-side. Switching is view-only: one
// instance is rendered at a time, one EventSource is held and re-subscribed on
// switch, and no cursor or sequence number is ever merged across instances.
//
// Rendering rules that mirror the terminal front end:
//   - The transcript is seeded from GET /api/history and extended by the
//     user_message / token / tool_call / tool_result / turn_done frames; a
//     reset_history frame replaces it.
//   - Scene areas are seeded from GET /api/scene and kept current from
//     ui_area_html (a full rendered region), ui_text_append (the streaming
//     fast path), ui_area_update, and ui_area_remove.
//   - Status, notices, the console pane, and the overlay surfaces (modal,
//     picker, agent tree, text input) mirror the harness widgets.
//
// Server-rendered scene HTML is trusted and inserted with innerHTML; every
// string that originates from a user or model message is inserted with
// textContent, never interpolated into markup.
const clientHTML = `<!DOCTYPE html>
<html lang="en">
<head>
<meta charset="utf-8">
<meta name="viewport" content="width=device-width, initial-scale=1">
<title>wllr</title>
<style>
  :root {
    --wn-accent:#89CFF0; --wn-muted:#888; --wn-error:#FF4444;
    --wn-success:#44CC66; --wn-warning:#E5C07B; --wn-fg:#e6e6e6; --wn-bg:#1A1A1A;
    --panel:#202020; --line:#333;
  }
  * { box-sizing: border-box; }
  body { margin:0; background:var(--wn-bg); color:var(--wn-fg);
    font:14px/1.45 ui-monospace, SFMono-Regular, Menlo, Consolas, monospace;
    display:flex; flex-direction:column; height:100vh; }
  #topbar { display:flex; align-items:center; gap:12px; padding:6px 10px;
    border-bottom:1px solid var(--line); background:var(--panel); }
  #topbar .spacer { flex:1; }
  #agent, #instance { background:var(--wn-bg); color:var(--wn-fg); border:1px solid var(--line);
    border-radius:4px; padding:2px 6px; }
  #working { color:var(--wn-accent); }
  #working.off { visibility:hidden; }
  #stage { flex:1; display:flex; flex-direction:row; min-height:0; }
  #maincol { flex:1; display:flex; flex-direction:column; min-width:0; }
  #transcript { flex:1; overflow-y:auto; padding:12px 14px; }
  #sidecol { width:300px; border-left:1px solid var(--line); overflow-y:auto;
    padding:8px; }
  #sidecol:empty { display:none; }
  .pane { margin:0 0 10px; border:1px solid var(--line); border-radius:6px;
    background:var(--panel); }
  .pane[hidden] { display:none; }
  .pane > header { display:flex; align-items:center; gap:8px; padding:5px 8px;
    border-bottom:1px solid var(--line); color:var(--wn-muted); font-size:12px;
    text-transform:uppercase; letter-spacing:.06em; }
  .pane > header .spacer { flex:1; }
  .pane > header button { padding:1px 8px; font-size:11px; text-transform:none;
    letter-spacing:0; }
  .pane ul { list-style:none; margin:0; padding:6px 8px; max-height:220px;
    overflow-y:auto; }
  .pane li { padding:2px 0; color:var(--wn-muted); font-size:12px;
    white-space:nowrap; overflow:hidden; text-overflow:ellipsis; }
  .pane li .n { color:var(--wn-fg); }
  .pane li.run .n { color:var(--wn-warning); }
  .pane li.ok .n { color:var(--wn-success); }
  .pane li.err .n { color:var(--wn-error); }
  .pane .empty { color:var(--wn-muted); font-size:12px; padding:6px 8px; }
  .msg { margin:0 0 12px; }
  .msg .who { color:var(--wn-muted); font-size:11px; text-transform:uppercase;
    letter-spacing:.08em; }
  .msg.user .body { color:var(--wn-accent,#89CFF0); }
  .msg .body { white-space:pre-wrap; overflow-wrap:anywhere; }
  .tool { margin:4px 0 10px; border-left:2px solid var(--wn-warning); padding-left:8px; }
  .tool .name { color:var(--wn-warning); }
  .tool pre { margin:4px 0 0; white-space:pre-wrap; overflow-wrap:anywhere;
    color:var(--wn-muted); }
  .tool.error .name { color:var(--wn-error); }
  #statusbar { border-top:1px solid var(--line); background:var(--panel);
    padding:4px 10px; display:flex; gap:14px; flex-wrap:wrap; min-height:22px; }
  #statusbar .chip { color:var(--wn-muted); }
  #statusbar .chip b { color:var(--wn-fg); font-weight:600; }
  #composer { display:flex; gap:8px; padding:10px; border-top:1px solid var(--line);
    background:var(--panel); }
  #input { flex:1; resize:vertical; min-height:44px; max-height:200px;
    background:var(--wn-bg); color:var(--wn-fg); border:1px solid var(--line);
    border-radius:4px; padding:8px; font:inherit; }
  #composer-wrap { position:relative; }
  #suggest { position:absolute; left:10px; right:10px; bottom:100%; margin-bottom:2px;
    background:var(--panel); border:1px solid var(--line); border-radius:6px;
    max-height:210px; overflow:auto; box-shadow:0 -4px 14px rgba(0,0,0,.4); z-index:5; }
  #suggest[hidden] { display:none; }
  #suggest .cmd { padding:5px 10px; cursor:pointer; display:flex; gap:10px;
    align-items:baseline; white-space:nowrap; overflow:hidden; }
  #suggest .cmd b { color:var(--wn-accent); font-weight:600; }
  #suggest .cmd .desc { color:var(--wn-muted); font-size:12px; overflow:hidden;
    text-overflow:ellipsis; }
  #suggest .cmd.sel { background:var(--wn-accent); }
  #suggest .cmd.sel b, #suggest .cmd.sel .desc { color:#111; }
  button { background:var(--wn-accent); color:#111; border:0; border-radius:4px;
    padding:8px 14px; font:inherit; font-weight:600; cursor:pointer; }
  button.ghost { background:transparent; color:var(--wn-fg); border:1px solid var(--line); }
  #overlay { position:fixed; inset:0; background:rgba(0,0,0,.55); display:none;
    align-items:center; justify-content:center; padding:24px; }
  #overlay.on { display:flex; }
  #overlay .card { background:var(--panel); border:1px solid var(--line);
    border-radius:8px; max-width:720px; width:100%; max-height:80vh; overflow:auto;
    padding:16px; }
  #overlay h2 { margin:0 0 10px; font-size:15px; }
  #overlay .item { padding:6px 8px; border-radius:4px; cursor:pointer; }
  #overlay .item:hover { background:#2c2c2c; }
  #overlay .item .sub { color:var(--wn-muted); font-size:12px; }
  #overlay .pre { white-space:pre-wrap; color:var(--wn-muted); margin-top:10px;
    border-top:1px solid var(--line); padding-top:10px; }
  #overlay pre { white-space:pre-wrap; }
  #overlay .row { display:flex; gap:8px; margin-top:12px; }
  #overlay .filter { width:100%; padding:6px 8px; margin-bottom:8px;
    background:var(--wn-bg); color:var(--wn-fg); border:1px solid var(--line);
    border-radius:4px; font:inherit; }
  #overlay .split { display:flex; gap:12px; align-items:stretch; }
  #overlay .split > .list { flex:1; min-width:0; max-height:52vh; overflow:auto; }
  #overlay .split > .preview { flex:1; min-width:0; border-left:1px solid var(--line);
    padding-left:12px; max-height:52vh; overflow:auto; white-space:pre-wrap;
    overflow-wrap:anywhere; color:var(--wn-muted); font-size:13px; }
  #overlay .split > .preview:empty::before { content:"\a0"; }
  #console { max-height:120px; overflow-y:auto; border-top:1px solid var(--line);
    padding:4px 10px; color:var(--wn-muted); font-size:12px; display:none; }
  #console.on { display:block; }
  .wn-node.wn-spinner { display:inline-block; width:1ch; }
  .wn-node.wn-spinner .wn-spin { display:inline-block; width:8px; height:8px;
    border-radius:50%; border:2px solid var(--wn-accent); border-top-color:transparent;
    animation:wnspin .7s linear infinite; }
  @keyframes wnspin { to { transform:rotate(360deg); } }
  .wn-node.wn-divider { border:0; border-top:1px solid var(--line); margin:4px 0; }
  /* When a chat scene area is present it owns the message transcript, so the
     fallback transcript collapses to just the tool/activity log (and vanishes
     entirely when empty). Without a chat area it is the transcript. */
  #transcript:empty { display:none; }
  #modelbtn { max-width:280px; overflow:hidden; text-overflow:ellipsis;
    white-space:nowrap; }
  #overlay a { color:var(--wn-accent); overflow-wrap:anywhere; }
  #overlay .item .sub.ok { color:var(--wn-success); }
  #overlay .item.active { background:#2c2c2c; }
  #overlay .item.active .sub { color:var(--wn-success); }
</style>
</head>
<body>
  <header id="topbar">
    <strong>wllr</strong>
    <select id="instance" title="switch instance"></select>
    <select id="agent" title="focus agent"></select>
    <button id="modelbtn" class="ghost" title="switch model">model</button>
    <button id="contextbtn" class="ghost" title="context breakdown">context</button>
    <span class="spacer"></span>
    <span id="usage"></span>
    <span id="working" class="off">streaming…</span>
  </header>
  <div id="stage">
    <div id="maincol">
      <div id="scene-main"></div>
      <div id="transcript"></div>
      <div id="scene-status"></div>
    </div>
    <aside id="sidecol">
      <div id="scene-side"></div>
      <section id="queue-pane" class="pane" hidden>
        <header>Queued (<span id="queue-count">0</span>)
          <span class="spacer"></span>
          <button type="button" class="ghost" id="queue-clear" title="discard queued messages">clear</button>
        </header>
        <ul id="queue-list"></ul>
      </section>
      <section id="tool-pane" class="pane" hidden>
        <header>Tools</header>
        <ul id="tool-list"></ul>
      </section>
    </aside>
  </div>
  <div id="console"></div>
  <footer id="statusbar"></footer>
  <form id="composer">
    <div id="suggest" hidden></div>
    <textarea id="input" placeholder="Message wllr  (Enter to send, Shift+Enter for newline, / for commands)"></textarea>
    <button type="submit" id="send">Send</button>
    <button type="button" class="ghost" id="cancel">Stop</button>
  </form>
  <div id="overlay"><div class="card" id="overlay-card"></div></div>

<script>
(function () {
  "use strict";
  var state = {
    focused: "main",
    agents: [],
    stream: {},      // agent id -> current assistant message element
    usage: null,
    statuses: {},
    provider: "",     // active provider (from GET /api/models)
    chatArea: false, // a chat scene area is driving the transcript
    inbox: [],       // messages queued for the focused agent
    tools: [],       // recent tool activity, newest last
    commands: [],    // registered slash commands (GET /api/commands)
    suggestions: [], // commands matching the in-progress /word
    sugIdx: 0,       // highlighted suggestion
    instance: "",    // selected instance id ("" until /api/instances answers)
    self: "",        // this origin's own instance id
    instances: [],   // [{id,self,status,protocol,last_seq}] from GET /api/instances
    reachable: true  // the selected instance's stream is connected
  };
  // es is the single EventSource. It is module-scope (not local to connect)
  // because a switch must be able to close it before re-subscribing.
  var es = null;
  var $ = function (id) { return document.getElementById(id); };

  // api prefixes a per-instance API path with the selected instance. The local
  // instance is served in-process, so it keeps the plain relative path; a peer
  // goes through the origin server's reverse proxy.
  function api(p) {
    return (state.instance === state.self || !state.instance)
      ? p
      : "api/instances/" + encodeURIComponent(state.instance) + "/" + p;
  }

  // stale reports whether a refresh issued for instance id has been overtaken by
  // a switch. Every refresh* (and every fetcher that opens an overlay) captures
  // state.instance when it starts and drops its response if the user switched
  // away, so a slow reply from instance A can never paint into instance B's view.
  function stale(id) { return id !== state.instance; }

  // instanceFromURL reads the deep-link ?instance= selection, and storedInstance
  // the last selection, so a reload or a shared link lands on the same instance.
  function instanceFromURL() {
    var m = /[?&]instance=([^&]+)/.exec(location.search);
    return m ? decodeURIComponent(m[1]) : "";
  }
  function storedInstance() {
    try { return localStorage.getItem("wllr.instance") || ""; } catch (e) { return ""; }
  }
  // rememberInstance makes the selection shareable and survives a reload. The page
  // is served only at "/", so a query parameter is safe and the relative base
  // stays "/". The new query is built from the existing one so unrelated params
  // survive: rewriting to "?instance=<id>" alone would silently drop a ?token=
  // (the only way the page can be reached under WLLR_API_TOKEN) and any future
  // parameter. The fragment is re-appended so a deep link's anchor survives too.
  function rememberInstance(id) {
    try { localStorage.setItem("wllr.instance", id); } catch (e) { /* private mode */ }
    if (window.history && history.replaceState) {
      var q = new URLSearchParams(location.search);
      q.set("instance", id);
      history.replaceState(null, "", "?" + q.toString() + location.hash);
    }
  }

  // esc is never used to build markup: text goes in via textContent. Kept for
  // the rare attribute assignment.
  function setText(el, text) { el.textContent = text == null ? "" : String(text); }

  function el(tag, cls, text) {
    var e = document.createElement(tag);
    if (cls) { e.className = cls; }
    if (text != null) { setText(e, text); }
    return e;
  }

  // --- transcript ---------------------------------------------------------
  function atBottom() {
    var t = $("transcript");
    return t.scrollHeight - t.scrollTop - t.clientHeight < 40;
  }
  function msgEl(role) {
    var wrap = el("div", "msg " + role);
    wrap.appendChild(el("div", "who", role === "user" ? "you" : "wllr"));
    wrap.appendChild(el("div", "body", ""));
    return wrap;
  }
  // sceneOwnsTranscript reports whether a chat scene area is driving the main
  // transcript (the bundled agents extension does this when enabled). When it
  // is, the fallback transcript must not also render messages or every message
  // would appear twice; it collapses to the tool/activity log instead.
  function sceneOwnsTranscript() { return state.chatArea === true; }

  // appendMessage adds a message bubble to the fallback transcript. html, when
  // present, is server-rendered markup (modules/server/mdhtml.go) and is the
  // only thing inserted as markup; otherwise content goes in as text. Returns
  // the body element, or null when the scene owns the transcript.
  function appendMessage(role, content, html) {
    if (sceneOwnsTranscript()) { return null; }
    var stick = atBottom();
    var wrap = msgEl(role);
    var body = wrap.querySelector(".body");
    if (html) { body.innerHTML = html; } else { setText(body, content); }
    $("transcript").appendChild(wrap);
    if (stick) { $("transcript").scrollTop = $("transcript").scrollHeight; }
    return body;
  }
  function appendToken(agent, text) {
    if (sceneOwnsTranscript()) { return; }
    var stick = atBottom();
    var body = state.stream[agent];
    if (!body) { body = appendMessage("assistant", ""); state.stream[agent] = body; }
    body.textContent += text;
    if (stick) { $("transcript").scrollTop = $("transcript").scrollHeight; }
  }
  function renderHistory(messages) {
    $("transcript").innerHTML = "";
    state.stream = {};
    if (sceneOwnsTranscript()) { return; }
    (messages || []).forEach(function (m) {
      if (m.type && m.type !== "normal" && m.type !== "steering") { return; }
      appendMessage(m.role === "user" ? "user" : "assistant", m.content, m.html);
    });
  }
  function appendTool(f) {
    var wrap = el("div", "tool" + (f.is_error ? " error" : ""));
    wrap.dataset.toolCallId = f.tool_call_id || "";
    var name = el("div", "name", (f.is_error ? "✗ " : "→ ") + (f.tool_name || "tool"));
    wrap.appendChild(name);
    if (f.input) { wrap.appendChild(el("pre", null, f.input)); }
    $("transcript").appendChild(wrap);
    $("transcript").scrollTop = $("transcript").scrollHeight;
  }
  function finishTool(f) {
    var sel = f.tool_call_id
      ? '.tool[data-tool-call-id="' + f.tool_call_id + '"]'
      : null;
    var node = sel ? $("transcript").querySelector(sel) : null;
    if (!node) { node = el("div", "tool"); $("transcript").appendChild(node); }
    if (f.is_error) { node.classList.add("error"); }
    if (f.output) { node.appendChild(el("pre", null, f.output)); }
  }

  // --- scene areas --------------------------------------------------------
  function slotFor(areaId) {
    var slot = document.querySelector('[data-area-slot="' + areaId + '"]');
    if (!slot) {
      slot = el("div");
      slot.dataset.areaSlot = areaId;
      $("scene-main").appendChild(slot);
    }
    return slot;
  }
  function placeSlot(area) {
    var slot = slotFor(area.id);
    var p = area.placement || "main";
    var host = p === "sidebar" ? $("scene-side")
      : p === "status" ? $("scene-status")
      : p === "overlay" ? $("scene-main")
      : $("scene-main");
    host.appendChild(slot);
    return slot;
  }
  // setChatArea records whether a chat scene area exists and, on a change,
  // re-seeds the transcript so the scene and the fallback transcript agree on
  // which one renders messages (exactly one of them does).
  function setChatArea(on) {
    if (state.chatArea === on) { return; }
    state.chatArea = on;
    $("transcript").innerHTML = "";
    state.stream = {};
    refreshHistory();
  }
  function applyAreaHTML(f) {
    var area = f.area || { id: f.area_id };
    if (!area || !area.id) { return; }
    var slot = placeSlot(area);
    slot.innerHTML = f.html || "";
    if (area.id === "chat") { setChatArea(true); }
  }
  function removeArea(id) {
    var slot = document.querySelector('[data-area-slot="' + id + '"]');
    if (slot) { slot.remove(); }
    if (id === "chat") { setChatArea(false); }
  }
  function appendText(f) {
    if (!f.area_id || !f.node_id) { return; }
    var slot = document.querySelector('[data-area-slot="' + f.area_id + '"]');
    if (!slot) { return; }
    var node = slot.querySelector('[data-node-id="' + f.node_id + '"] .wn-text-content');
    if (node) { node.textContent += (f.text || ""); }
  }

  // --- agents / status ----------------------------------------------------
  function renderAgents(agents) {
    state.agents = agents || [];
    var sel = $("agent");
    sel.innerHTML = "";
    state.agents.forEach(function (a) {
      var o = el("option", null, a.name || a.id);
      o.value = a.id;
      sel.appendChild(o);
    });
    sel.value = state.focused;
  }
  function refreshAgents() {
    var id = state.instance;
    return fetch(api("api/agents")).then(function (r) { return r.json(); })
      .then(function (d) { if (stale(id)) { return; } renderAgents(d.agents); })
      .catch(function () {});
  }
  function refreshHistory() {
    var id = state.instance;
    return fetch(api("api/history?agent=" + encodeURIComponent(state.focused)))
      .then(function (r) { return r.json(); })
      .then(function (d) { if (stale(id)) { return; } renderHistory(d.messages); })
      .catch(function () {});
  }
  function refreshScene() {
    var id = state.instance;
    return fetch(api("api/scene")).then(function (r) { return r.json(); })
      .then(function (d) {
        if (stale(id)) { return; }
        (d.areas || []).forEach(function (a) { applyAreaHTML({ area: a.area, html: a.html }); });
      })
      .catch(function () {});
  }

  // --- queued-message pane ------------------------------------------------
  // Mirrors the terminal's queued-messages pane: the messages an agent has
  // accepted but not yet drained, with a clickable clear. The data comes from
  // GET /api/inbox (the same pool snapshot the TUI reads); clearing posts to
  // /api/inbox/clear. The pane hides when the queue is empty, exactly as the
  // TUI hides it.
  function refreshInbox() {
    var id = state.instance;
    return fetch(api("api/inbox?agent=" + encodeURIComponent(state.focused)))
      .then(function (r) { return r.json(); })
      .then(function (d) { if (stale(id)) { return; } renderInbox(d.messages || []); })
      .catch(function () {});
  }
  function renderInbox(msgs) {
    state.inbox = msgs;
    var pane = $("queue-pane");
    if (!msgs.length) { pane.hidden = true; $("queue-list").innerHTML = ""; return; }
    pane.hidden = false;
    setText($("queue-count"), String(msgs.length));
    var list = $("queue-list");
    list.innerHTML = "";
    // Show the latest messages; surface a hint when older ones are hidden,
    // matching the TUI's "N older message(s)" line.
    var max = 8;
    var hidden = msgs.length - max;
    if (hidden > 0) {
      list.appendChild(el("li", null, "\u2026 " + hidden + " older message(s) in queue"));
    }
    msgs.slice(Math.max(0, hidden)).forEach(function (m) {
      var oneLine = String(m.content || "").replace(/\s+/g, " ").trim();
      list.appendChild(el("li", null, "queued " + oneLine));
    });
  }
  function clearInbox() {
    fetch(api("api/inbox/clear"), {
      method: "POST",
      headers: { "Content-Type": "application/json" },
      body: JSON.stringify({ agent: state.focused })
    }).then(refreshInbox).catch(function () {});
  }

  // --- tool-activity pane -------------------------------------------------
  // Mirrors the terminal's tools pane: a rolling list of recent tool activity,
  // newest last, marked running/ok/error. Entries are matched by tool_call_id
  // so a result updates the row its call created rather than appending a new
  // one. The pane persists across a scene-owned transcript because it is not
  // part of the chat area.
  function toolActivityBegin(f) {
    state.tools.push({
      id: f.tool_call_id || ("t" + state.tools.length),
      agent: f.agent_id || "",
      name: f.tool_name || "tool",
      status: "run"
    });
    if (state.tools.length > 50) { state.tools = state.tools.slice(-50); }
    renderTools();
  }
  function toolActivityEnd(f) {
    var id = f.tool_call_id;
    for (var i = state.tools.length - 1; i >= 0; i--) {
      if (state.tools[i].id === id) {
        state.tools[i].status = f.is_error ? "err" : "ok";
        renderTools();
        return;
      }
    }
    state.tools.push({ id: id || "", agent: f.agent_id || "",
      name: f.tool_name || "tool", status: f.is_error ? "err" : "ok" });
    renderTools();
  }
  function renderTools() {
    var pane = $("tool-pane");
    if (!state.tools.length) { pane.hidden = true; $("tool-list").innerHTML = ""; return; }
    pane.hidden = false;
    var list = $("tool-list");
    list.innerHTML = "";
    state.tools.forEach(function (t) {
      var mark = t.status === "run" ? "\u2192 " : t.status === "err" ? "\u2717 " : "\u2713 ";
      var row = el("li", t.status);
      row.appendChild(el("span", "n", mark + t.name));
      if (t.agent && t.agent !== state.focused) {
        row.appendChild(document.createTextNode("  " + t.agent));
      }
      list.appendChild(row);
    });
    list.scrollTop = list.scrollHeight;
  }

  // --- model selection ----------------------------------------------------
  // The list and the callback id come from the server (GET /api/models), so the
  // browser never hardcodes the picker-key encoding: it echoes back the key the
  // server built, exactly as the terminal picker would.
  function openModelPicker() {
    var id = state.instance;
    fetch(api("api/models")).then(function (r) { return r.json(); }).then(function (d) {
      if (stale(id)) { return; }
      var models = d.models || [];
      if (!models.length) { consoleLine("[wllr] no models configured", false); return; }
      openOverlay(function (card) {
        card.appendChild(el("h2", null, "Models"));
        models.forEach(function (m) {
          var row = el("div", "item" + (m.active ? " active" : ""));
          row.appendChild(el("div", null, m.name || m.id));
          var sub = m.sublabel || "";
          if (m.provider) { sub = sub ? sub + " · " + m.provider : m.provider; }
          if (m.tiers && m.tiers.length) {
            sub = sub ? sub + " · tier: " + m.tiers.join(", ") : "tier: " + m.tiers.join(", ");
          }
          if (m.active) { sub = sub ? sub + " · current" : "current"; }
          if (sub) { row.appendChild(el("div", "sub", sub)); }
          row.onclick = function () {
            routeCallback(d.callback, m.key);
            closeOverlay();
            setTimeout(refreshModels, 400);
          };
          card.appendChild(row);
        });
        card.appendChild(el("div", "row")).appendChild(closeBtn());
      });
    }).catch(function () { consoleLine("[wllr] could not load models", false); });
  }
  function refreshModels() {
    var id = state.instance;
    return fetch(api("api/models")).then(function (r) { return r.json(); })
      .then(function (d) {
        if (stale(id)) { return; }
        state.provider = (d.current || {}).provider || "";
        var active = (d.models || []).filter(function (m) { return m.active; })[0];
        var label = active ? (active.name || active.id) : ((d.current || {}).model || "model");
        $("modelbtn").textContent = label;
      })
      .catch(function () {});
  }

  // --- context breakdown --------------------------------------------------
  // The /context modal. The breakdown is engine data (GET /api/context), so the
  // browser renders the same composition view the terminal shows: the window,
  // the last request's usage (labeled provider-reported or the chars/4 seed),
  // the estimated next-request breakdown, and the not-in-context canonical
  // transcript. Numbers are formatted with thousands separators here, as the
  // terminal does; the formatting is presentation, not logic.
  function comma(n) {
    var s = String(Math.trunc(n || 0));
    var neg = s.charAt(0) === "-";
    if (neg) { s = s.slice(1); }
    var out = "";
    while (s.length > 3) { out = "," + s.slice(-3) + out; s = s.slice(0, -3); }
    return (neg ? "-" : "") + s + out;
  }
  function pad(s, n) { s = String(s); return s.length >= n ? s : s + " ".repeat(n - s.length); }
  // labeled renders "<label padded to width><value right-aligned in 16>".
  function line(label, value, width) {
    return "  " + pad(label, width || 18) + pad(comma(value), 0);
  }
  function buildContextText(c) {
    var out = [];
    out.push("Context window: " + (c.context_window > 0 ? comma(c.context_window) + " tokens" : "unknown"));
    var lr = c.last_request || {};
    out.push("");
    out.push("Last request" + (lr.is_estimate ? " (chars/4 baseline \u2014 no provider report yet)" : " (provider-reported)"));
    if (!lr.input_tokens) {
      out.push("  No completed turn yet.");
    } else {
      var inLine = "  input " + comma(lr.input_tokens);
      if (c.context_window > 0) {
        inLine += "  (" + Math.round(100 * lr.input_tokens / c.context_window) + "% of window)";
      }
      out.push(inLine);
      if (lr.cache_read_tokens || lr.cache_creation_tokens) {
        out.push("  cache read " + comma(lr.cache_read_tokens) +
          " \u00b7 cache write " + comma(lr.cache_creation_tokens));
      }
      out.push("  output " + comma(lr.output_tokens));
    }
    out.push("");
    out.push("Next request estimate (chars/4 heuristic)");
    out.push(line("system prompt", c.system_tokens));
    (c.system_components || []).forEach(function (comp, i) {
      if (i === 0) { out.push("    by source:"); }
      out.push("      " + pad(comp.source || "(unlabeled)", 30) + comma(comp.tokens));
    });
    out.push(line("tools (" + (c.tool_count || 0) + ")", c.tool_tokens));
    (c.tools_by_tool || []).forEach(function (t) {
      var name = t.extension ? t.extension + "/" + t.name : t.name;
      out.push("    " + pad(name, 20) + comma(t.tokens));
    });
    out.push("  " + pad("history", 18) + comma(c.history_tokens) +
      (c.turn_count > 0 ? "  (" + c.turn_count + " turns: user " + comma(c.user_tokens) +
        ", assistant " + comma(c.assistant_tokens) + ")" : ""));
    (c.largest_messages || []).forEach(function (m, i) {
      if (i === 0) { out.push("  largest prompts:"); }
      out.push("    " + pad(m.role, 10) + comma(m.tokens));
    });
    if (c.filtered_tokens > 0) {
      out.push(line("filtered (control, not sent)", c.filtered_tokens, 30));
    }
    out.push(line("estimated total", c.estimated_total));
    if (c.canonical) {
      out.push("");
      out.push("Not in context");
      out.push("  canonical transcript: " + comma(c.canonical.chars) + " chars across " +
        c.canonical.entries + " entries \u2014 verbatim detail");
      if (c.canonical.recallable) { out.push("  folded out of context is recoverable via the recall tool."); }
      var kinds = { message: "messages", tool_call: "tool calls", tool_result: "tool results" };
      var byKind = c.canonical.by_kind || {};
      var anyKind = Object.keys(byKind).some(function (k) { return byKind[k] > 0; });
      if (anyKind) {
        out.push("  by type:");
        Object.keys(kinds).forEach(function (k) {
          if (byKind[k] > 0) {
            out.push("    " + pad(kinds[k], 20) + comma(byKind[k]));
            if (k === "message") {
              var roles = c.canonical.by_role || {};
              if (roles.user > 0) { out.push("      " + pad("user", 16) + comma(roles.user)); }
              if (roles.assistant > 0) { out.push("      " + pad("assistant", 16) + comma(roles.assistant)); }
            }
          }
        });
      }
      var byTool = c.canonical.by_tool || [];
      if (byTool.length) {
        out.push("  tool traffic by tool:");
        byTool.slice(0, 10).forEach(function (t) {
          out.push("    " + pad(t.name, 20) + comma(t.chars));
        });
        if (byTool.length > 10) { out.push("    \u2026 and " + (byTool.length - 10) + " more tools"); }
      }
    }
    return out.join("\n");
  }
  function openContextModal() {
    var id = state.instance;
    fetch(api("api/context?agent=" + encodeURIComponent(state.focused)))
      .then(function (r) { return r.json(); })
      .then(function (c) {
        if (stale(id)) { return; }
        openOverlay(function (card) {
          card.appendChild(el("h2", null, "/context \u2014 " + (c.agent_id || state.focused)));
          card.appendChild(el("pre", null, buildContextText(c)));
          card.appendChild(el("div", "row")).appendChild(closeBtn());
        });
      })
      .catch(function () { consoleLine("[wllr] could not load context", false); });
  }

  function renderStatus() {
    var bar = $("statusbar");
    bar.innerHTML = "";
    Object.keys(state.statuses).sort().forEach(function (k) {
      var chip = el("span", "chip");
      chip.appendChild(document.createTextNode(k + ": "));
      chip.appendChild(el("b", null, state.statuses[k]));
      bar.appendChild(chip);
    });
  }
  function renderUsage() {
    var u = state.usage;
    $("usage").textContent = u && u.limit
      ? "ctx " + Math.round((u.used / u.limit) * 100) + "%"
      : "";
  }
  function setWorking(on) {
    $("working").className = on ? "" : "off";
  }

  // --- overlays -----------------------------------------------------------
  function closeOverlay() { $("overlay").className = ""; $("overlay-card").innerHTML = ""; }
  function openOverlay(build) {
    var card = $("overlay-card");
    card.innerHTML = "";
    build(card);
    $("overlay").className = "on";
  }
  // pickerOverlay renders the interactive picker. When the frame requests a
  // split layout (p.split — the terminal's two-pane picker) it shows a
  // type-to-filter list beside a preview pane for the highlighted item, with
  // keyboard navigation; otherwise it is the plain list. The preview falls back
  // to sublabel, matching the terminal (picker.go previewPaneLines).
  function pickerOverlay(p) {
    if (p.split) { splitPickerOverlay(p); return; }
    openOverlay(function (card) {
      card.appendChild(el("h2", null, p.title || "Select"));
      (p.items || []).forEach(function (it) {
        var row = el("div", "item");
        row.appendChild(el("div", null, it.label || it.id));
        if (it.sublabel) { row.appendChild(el("div", "sub", it.sublabel)); }
        row.onclick = function () {
          routeCallback(p.callback, it.id);
          closeOverlay();
        };
        card.appendChild(row);
      });
      card.appendChild(el("div", "row")).appendChild(closeBtn());
    });
  }
  // splitPickerOverlay is the two-pane picker. Filtering matches label, id,
  // sublabel, and preview text (the terminal folds preview into the haystack so
  // a user can search by the previewed content).
  function splitPickerOverlay(p) {
    var items = p.items || [];
    openOverlay(function (card) {
      card.appendChild(el("h2", null, p.title || "Select"));
      var filter = el("input", "filter");
      filter.placeholder = "filter…";
      filter.type = "text";
      card.appendChild(filter);
      var split = el("div", "split");
      var list = el("div", "list");
      var preview = el("div", "preview");
      split.appendChild(list);
      split.appendChild(preview);
      card.appendChild(split);
      card.appendChild(el("div", "row")).appendChild(closeBtn());

      var shown = items.slice();
      var sel = 0;
      function visibleItems() {
        var q = filter.value.trim().toLowerCase();
        if (!q) { return items; }
        return items.filter(function (it) {
          var hay = ((it.label || "") + " " + it.id + " " +
            (it.sublabel || "") + " " + (it.preview || "")).toLowerCase();
          return hay.indexOf(q) >= 0;
        });
      }
      function paint() {
        shown = visibleItems();
        if (sel >= shown.length) { sel = shown.length ? shown.length - 1 : 0; }
        list.innerHTML = "";
        shown.forEach(function (it, i) {
          var row = el("div", "item" + (i === sel ? " active" : ""));
          row.appendChild(el("div", null, it.label || it.id));
          if (it.sublabel) { row.appendChild(el("div", "sub", it.sublabel)); }
          row.onclick = function () { choose(it); };
          list.appendChild(row);
        });
        var cur = shown[sel];
        setText(preview, cur ? (cur.preview || cur.sublabel || "") : "");
      }
      function choose(it) {
        if (!it) { return; }
        routeCallback(p.callback, it.id);
        closeOverlay();
      }
      function move(d) {
        if (!shown.length) { return; }
        sel = (sel + d + shown.length) % shown.length;
        paint();
      }
      filter.addEventListener("input", function () { sel = 0; paint(); });
      filter.addEventListener("keydown", function (e) {
        if (e.key === "ArrowDown") { e.preventDefault(); move(1); }
        else if (e.key === "ArrowUp") { e.preventDefault(); move(-1); }
        else if (e.key === "Enter") { e.preventDefault(); choose(shown[sel]); }
      });
      paint();
      filter.focus();
    });
  }
  function closeBtn() {
    var b = el("button", "ghost", "Close");
    b.onclick = closeOverlay;
    return b;
  }
  function textInputOverlay(f) {
    var p = f.text_input || {};
    openOverlay(function (card) {
      card.appendChild(el("h2", null, p.title || "Input"));
      var inp = el("input");
      inp.value = p.initialValue || "";
      inp.placeholder = p.placeholder || "";
      inp.style.width = "100%";
      inp.style.padding = "6px";
      card.appendChild(inp);
      function submit() {
        routeCallback(p.callback, inp.value);
        closeOverlay();
      }
      inp.addEventListener("keydown", function (e) {
        if (e.key === "Enter") { e.preventDefault(); submit(); }
      });
      var row = el("div", "row");
      var ok = el("button", null, "Submit");
      ok.onclick = submit;
      row.appendChild(ok);
      row.appendChild(closeBtn());
      card.appendChild(row);
      inp.focus();
    });
  }
  function treeOverlay(f) {
    var p = f.agent_tree || {};
    openOverlay(function (card) {
      card.appendChild(el("h2", null, p.title || "Agents"));
      (p.nodes || []).forEach(function (n) {
        var row = el("div", "item");
        row.appendChild(el("div", null, n.label || n.id));
        if (n.id && n.id !== (n.label || "")) {
          row.appendChild(el("div", "sub", n.id));
        }
        row.onclick = function () {
          routeCallback(p.callback, n.id);
          closeOverlay();
        };
        card.appendChild(row);
      });
      card.appendChild(el("div", "row")).appendChild(closeBtn());
    });
  }
  function consoleLine(text, clear) {
    var c = $("console");
    c.className = "on";
    if (clear) { c.innerHTML = ""; }
    c.appendChild(el("div", null, text));
    c.scrollTop = c.scrollHeight;
  }

  // --- stream -------------------------------------------------------------
  function handle(f) {
    switch (f.kind) {
      case "ready":
        setInstanceReachable(true);
        // Refresh the selector on every ready frame: a peer that has recovered
        // must lose its "(down)" label without requiring a switch, and the page
        // does not poll.
        refreshInstances();
        if (f.protocol && f.protocol !== __PROTOCOL__) {
          consoleLine("[warn] instance " + state.instance + " protocol " + f.protocol +
            ", client expects " + __PROTOCOL__, false);
        }
        break;
      case "user_message":
        appendMessage("user", (f.inbox && f.inbox.length)
          ? f.inbox.map(function (m) { return m.content; }).join("\n")
          : (f.text || ""));
        state.stream[f.agent_id] = null;
        setWorking(true);
        if ((f.agent_id || "main") === state.focused) { refreshInbox(); }
        break;
      case "turn_started": setWorking(true); break;
      case "token": appendToken(f.agent_id, f.text || ""); break;
      case "assistant_message":
        // The turn's streamed text, rendered server-side. Replace the in-flight
        // bubble's raw text with the rendered form — the same swap the terminal
        // transcript makes: raw while streaming, rendered once final. The scene
        // path (chat area) already renders it, so this is a no-op there.
        if (!sceneOwnsTranscript()) {
          var abody = state.stream[f.agent_id];
          if (abody) { abody.innerHTML = f.html || ""; }
          else if (f.html) { appendMessage("assistant", f.text || "", f.html); }
        }
        break;
      case "turn_done":
        state.stream[f.agent_id] = null;
        setWorking(false);
        if (f.err) { appendMessage("assistant", "[error] " + f.err); }
        if ((f.agent_id || "main") === state.focused) { refreshInbox(); }
        break;
      case "tool_call": appendTool(f); toolActivityBegin(f); break;
      case "tool_result": finishTool(f); toolActivityEnd(f); break;
      case "context_usage": state.usage = f.usage; renderUsage(); break;
      case "notify":
        if (!sceneOwnsTranscript()) { appendMessage("assistant", "• " + (f.text || "")); }
        break;
      case "status": state.statuses[f.key] = f.value; renderStatus(); break;
      case "console": consoleLine(f.text || "", false); break;
      case "console_clear": consoleLine("", true); break;
      case "reset_history": renderHistory(f.messages); break;
      case "agent_spawned": case "agent_closed": refreshAgents(); break;
      case "ui_area": case "ui_area_update": applyAreaHTML(f); break;
      case "ui_area_html": applyAreaHTML(f); break;
      case "ui_text_append": appendText(f); break;
      case "ui_area_remove": removeArea(f.text); break;
      case "modal":
        openOverlay(function (card) {
          card.appendChild(el("pre", null, f.text || ""));
          card.appendChild(el("div", "row")).appendChild(closeBtn());
        });
        break;
      case "picker": pickerOverlay(f.picker || {}); break;
      case "text_input": textInputOverlay(f); break;
      case "agent_tree": treeOverlay(f); break;
      case "abort": setWorking(false); break;
      case "error": consoleLine("[error] " + (f.text || ""), false); break;
    }
  }

  // connect opens the single stream for the selected instance. A switch closes
  // this one and calls connect again, so there is never more than one
  // EventSource and no cursor is shared across instances.
  function connect() {
    es = new EventSource(api("api/stream"));
    es.onmessage = function (ev) {
      var f;
      try { f = JSON.parse(ev.data); } catch (e) { return; }
      handle(f);
    };
    es.onerror = function () {
      setInstanceReachable(false);
      refreshInstances();
    };
  }

  // setInstanceReachable disables the composer while the selected instance's
  // stream is down, so a dead peer cannot silently swallow input, and re-enables
  // it when its stream reports ready.
  function setInstanceReachable(on) {
    if (state.reachable === on) { return; }
    state.reachable = on;
    $("input").disabled = !on;
    $("send").disabled = !on;
  }

  // refreshInstances loads GET /api/instances (always the origin route, never
  // prefixed) and rebuilds the selector.
  function refreshInstances() {
    return fetch("api/instances").then(function (r) { return r.json(); })
      .then(function (d) {
        state.self = d.self || "";
        state.instances = d.instances || [];
        renderInstances();
      })
      .catch(function () {});
  }
  function renderInstances() {
    var sel = $("instance");
    sel.innerHTML = "";
    state.instances.forEach(function (inst) {
      var label = inst.status === "ok" ? inst.id : inst.id + " (" + inst.status + ")";
      var o = el("option", null, label);
      o.value = inst.id;
      sel.appendChild(o);
    });
    sel.value = state.instance;
  }
  // knownInstance reports whether id is one of the instances the origin lists,
  // so an unknown deep link falls back to self instead of wedging the page.
  function knownInstance(id) {
    return state.instances.some(function (inst) { return inst.id === id; });
  }

  // switchInstance shows exactly one instance's view. It closes the single stream,
  // clears every per-instance surface (resetView) so instance A's transcript,
  // scene, statuses, queue, tools, usage, and focus can never appear in B's view,
  // then re-seeds from the new instance and re-subscribes.
  function switchInstance(id) {
    if (!knownInstance(id)) { id = state.self; }
    if (!id) { return; }
    if (es) { es.close(); es = null; }
    state.instance = id;
    $("instance").value = id;
    resetView();
    rememberInstance(id);
    connect();
    refreshAgents().then(refreshHistory);
    refreshScene();
    refreshModels();
    refreshInbox();
    refreshCommands();
  }

  // resetView clears every per-instance surface before the new instance is
  // seeded, so nothing from the previous instance can leak into this one. The
  // fallback transcript and the three scene containers are cleared wholesale,
  // which removes every scene area and its slot.
  function resetView() {
    state.focused = "main";
    state.agents = [];
    state.stream = {};
    state.usage = null;
    state.statuses = {};
    state.provider = "";
    state.chatArea = false;
    state.inbox = [];
    state.tools = [];
    state.commands = [];
    state.suggestions = [];
    state.sugIdx = 0;
    $("transcript").innerHTML = "";
    $("scene-main").innerHTML = "";
    $("scene-side").innerHTML = "";
    $("scene-status").innerHTML = "";
    $("queue-pane").hidden = true;
    $("queue-list").innerHTML = "";
    $("tool-pane").hidden = true;
    $("tool-list").innerHTML = "";
    $("agent").innerHTML = "";
    $("statusbar").innerHTML = "";
    $("modelbtn").textContent = "model";
    $("usage").textContent = "";
    $("console").innerHTML = "";
    $("console").className = "";
    // Set the DOM directly, not via setInstanceReachable(true): that early-returns
    // when state.reachable is already true and would leave a down instance's
    // disabled composer disabled forever.
    $("input").disabled = false;
    $("send").disabled = false;
    state.reachable = true;
    setWorking(false);
    closeOverlay();
    closeSuggest();
  }

  // --- composer -----------------------------------------------------------
  function routeCallback(callback, value, args) {
    if (!callback) { return; }
    var body = { callback: callback, value: value };
    if (args && args.length) { body.args = args; }
    fetch(api("api/ui/callback"), {
      method: "POST",
      headers: { "Content-Type": "application/json" },
      body: JSON.stringify(body)
    }).catch(function () {});
  }
  function refreshCommands() {
    var id = state.instance;
    return fetch(api("api/commands")).then(function (r) { return r.json(); })
      .then(function (d) { if (stale(id)) { return; } state.commands = d.commands || []; })
      .catch(function () {});
  }

  // --- slash-command autocomplete ----------------------------------------
  // slashWordAt mirrors sdk.SlashWordAt exactly: the dropdown must appear under
  // the same conditions in the browser as in the terminal. A slash word starts
  // at the beginning or after a space and has no space before the caret.
  function slashWordAt(val) {
    var idx = val.lastIndexOf("/");
    if (idx < 0) { return -1; }
    if (idx > 0 && val.charAt(idx - 1) !== " ") { return -1; }
    if (val.slice(idx + 1).indexOf(" ") >= 0) { return -1; }
    return idx;
  }
  function updateSuggestions() {
    var val = $("input").value;
    var idx = slashWordAt(val);
    if (idx < 0) { closeSuggest(); return; }
    var q = val.slice(idx + 1).toLowerCase();
    var m = [];
    state.commands.forEach(function (c) {
      if (c.name.toLowerCase().indexOf(q) === 0) { m.push(c); }
    });
    state.suggestions = m;
    if (state.sugIdx >= m.length) { state.sugIdx = 0; }
    renderSuggest();
  }
  function closeSuggest() {
    state.suggestions = [];
    state.sugIdx = 0;
    var s = $("suggest");
    s.hidden = true;
    s.textContent = "";
  }
  function suggestOpen() { return state.suggestions.length > 0; }
  function renderSuggest() {
    var s = $("suggest");
    if (!suggestOpen()) { closeSuggest(); return; }
    s.textContent = "";
    state.suggestions.forEach(function (c, i) {
      var row = el("div", "cmd" + (i === state.sugIdx ? " sel" : ""));
      var name = el("b", "", "/" + c.name);
      row.appendChild(name);
      if (c.desc) { row.appendChild(el("span", "desc", c.desc)); }
      row.addEventListener("mousedown", function (e) {
        e.preventDefault();
        completeSuggestion(c);
      });
      s.appendChild(row);
    });
    s.hidden = false;
  }
  // completeSuggestion replaces the in-progress /word with the chosen command.
  // A trailing space is added so the caret is ready for arguments, matching the
  // terminal's Tab completion.
  function completeSuggestion(c) {
    var inp = $("input");
    var val = inp.value;
    var idx = slashWordAt(val);
    if (idx < 0) { return; }
    inp.value = val.slice(0, idx) + "/" + c.name + " ";
    closeSuggest();
    inp.focus();
  }
  function moveSuggestion(delta) {
    var n = state.suggestions.length;
    if (n === 0) { return; }
    state.sugIdx = (state.sugIdx + delta + n) % n;
    renderSuggest();
  }

  // --- command dispatch ---------------------------------------------------
  // A few commands are purely about opening a part of this front end and are
  // handled locally (the terminal opens the same picker/modal). Everything else
  // is a selection or an extension command, routed through the callback endpoint
  // the same way a click is, so it reaches the identical engine handler.
  var localCommands = {
    help: function () { openHelp(); },
    model: function (args) { if (args) { routeCallback("__wllr:model", args); } else { openModelPicker(); } },
    models: function (args) { if (args) { routeCallback("__wllr:model", args); } else { openModelPicker(); } },
    context: function () { openContextModal(); },
    tools: function () { toggleTools(); }
  };
  function knownCommand(name) {
    var found = false;
    state.commands.forEach(function (c) { if (c.name === name) { found = true; } });
    return found;
  }
  // runCommand returns true when the input was a slash command (handled here or
  // routed) and false when it should be submitted as a chat message.
  function runCommand(content) {
    if (content.charAt(0) !== "/") { return false; }
    var rest = content.slice(1);
    var sp = rest.indexOf(" ");
    var name = (sp < 0 ? rest : rest.slice(0, sp)).toLowerCase();
    var args = (sp < 0 ? "" : rest.slice(sp + 1)).trim();
    if (!name) { return false; }
    var local = localCommands[name];
    if (local) { local(args); return true; }
    if (name === "thinking" && args) { routeCallback("__wllr:thinking", args); return true; }
    if (name === "openrouter-speed" && args) { routeCallback("__wllr:speed", args); return true; }
    if (knownCommand(name)) {
      // An extension command (for example /history): dispatch through the same
      // host event path an extension button uses.
      routeCallback(name, args);
      return true;
    }
    consoleLine("unknown command: /" + name, false);
    return true;
  }
  function openHelp() {
    var cmds = state.commands;
    openOverlay(function (card) {
      card.appendChild(el("h2", null, "Commands"));
      if (cmds.length === 0) {
        card.appendChild(el("div", "sub", "No commands registered."));
      }
      cmds.forEach(function (c) {
        var row = el("div", "item");
        row.appendChild(el("div", null, "/" + c.name));
        if (c.desc) { row.appendChild(el("div", "sub", c.desc)); }
        card.appendChild(row);
      });
      card.appendChild(el("div", "row")).appendChild(closeBtn());
    });
  }
  function toggleTools() {
    var p = $("tool-pane");
    p.hidden = !p.hidden;
  }

  function submit() {
    var input = $("input");
    var content = input.value.trim();
    if (!content) { return; }
    if (runCommand(content)) { input.value = ""; closeSuggest(); return; }
    input.value = "";
    closeSuggest();
    fetch(api("api/submit"), {
      method: "POST",
      headers: { "Content-Type": "application/json" },
      body: JSON.stringify({ agent: state.focused, content: content })
    }).then(refreshInbox).catch(function () {});
  }
  function cancel() {
    fetch(api("api/cancel"), {
      method: "POST",
      headers: { "Content-Type": "application/json" },
      body: JSON.stringify({ agent: state.focused })
    }).catch(function () {});
  }

  // --- init ---------------------------------------------------------------
  $("composer").addEventListener("submit", function (e) { e.preventDefault(); submit(); });
  $("cancel").addEventListener("click", cancel);
  $("input").addEventListener("input", updateSuggestions);
  $("input").addEventListener("keydown", function (e) {
    if (suggestOpen()) {
      if (e.key === "ArrowDown") { e.preventDefault(); moveSuggestion(1); return; }
      if (e.key === "ArrowUp") { e.preventDefault(); moveSuggestion(-1); return; }
      if (e.key === "Tab") {
        e.preventDefault();
        completeSuggestion(state.suggestions[state.sugIdx]);
        return;
      }
      if (e.key === "Escape") { e.preventDefault(); closeSuggest(); return; }
    }
    if (e.key === "Enter" && !e.shiftKey) { e.preventDefault(); submit(); }
  });
  $("agent").addEventListener("change", function (e) {
    state.focused = e.target.value;
    refreshHistory();
    refreshInbox();
    renderTools();
  });
  $("instance").addEventListener("change", function (e) {
    switchInstance(e.target.value);
  });
  $("queue-clear").addEventListener("click", clearInbox);
  $("modelbtn").addEventListener("click", openModelPicker);
  $("contextbtn").addEventListener("click", openContextModal);
  document.addEventListener("keydown", function (e) {
    if (e.key === "Escape") { closeSuggest(); closeOverlay(); }
  });

  fetch("api/health").then(function (r) { return r.json(); })
    .then(function (h) {
      var expect = __PROTOCOL__;
      if (h.protocol !== expect) {
        consoleLine("[warn] server protocol " + h.protocol + ", client expects " + expect, false);
      }
    }).catch(function () {});
  var target = instanceFromURL() || storedInstance();
  refreshInstances().then(function () { switchInstance(target || state.self); });
})();
</script>
</body>
</html>
`
