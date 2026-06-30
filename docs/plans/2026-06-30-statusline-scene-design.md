# Statusline Scene Design

*Date: 2026-06-30*

## Overview

Replace the current `StatusBar` struct and `SetStatusLine`/`SetStatus` ABI with a fully
scene-graph-driven statusline. The statusline becomes a first-class predefined scene area
(`"statusline"`) that the harness composites as a standalone row above the input box. A
bundled WASM extension (`extensions/statusline/`) owns the default content and patches the
area via the standard `ui_patch` host call. Other extensions can inject their own segments
by patching the same area.

This is the natural continuation of UI P4 (WASM-driven chat transcript, SPECS.md §27): the
same mechanism that lets the `agents` extension own the chat transcript now lets the
`statusline` extension — and any other extension — own and extend the status row.

---

## Motivation

The current statusline is a Go struct (`StatusBar`) embedded in the input box top border.
Extensions can influence it only via two blunt instruments:

- `SetStatus(key, value)` — injects a `key:value` token into a sorted list
- `SetStatusLine(text)` — replaces the whole line verbatim

Neither allows structured layout, styling, or multi-line output. There is no way for two
extensions to independently contribute well-positioned, styled segments without knowing
about each other. Moving to the scene graph gives extensions the full `UINode`/`UIProps`
vocabulary (hstack, vstack, text, dividers, colours, borders, wrap) and the incremental
patch API they already use for other UI.

---

## New Layout

Three predefined windows are established as well-known scene area IDs:

```
┌──────────────────────────────────────┐
│  chat log  (scrollable viewport)     │  area: "chat",       placement: main
├──────────────────────────────────────┤
│  statusline  (1–N lines)             │  area: "statusline", placement: status
├──────────────────────────────────────┤
│  input box  (5 lines, plain border)  │  harness-owned, non-scene
└──────────────────────────────────────┘
```

| Area ID        | Placement       | Owner                        | Notes |
|----------------|-----------------|------------------------------|-------|
| `"chat"`       | `UIAreaMain`    | `agents` WASM extension      | Unchanged from UI P4 |
| `"statusline"` | `UIAreaStatus`  | `statusline` WASM extension  | New — this design |
| *(input box)*  | `UIAreaInput`   | harness (non-scene)          | Textarea is interactive; cannot be a passive scene node |

`UIAreaInput` is added as a placement constant so extensions can document/query its logical
slot, even though its content is always harness-owned.

### Statusline height

The statusline area height is **dynamic**. After rendering the area to a string the harness
counts newlines and subtracts that from the chat viewport height. Extensions can grow the
statusline to multiple lines (e.g. a second row for active sub-agents) or collapse it to
zero lines when idle.

Height and width can be constrained at area creation time and updated later (see §Area
Sizing Constraints below).

---

## Area Sizing Constraints

### UIArea changes

`UIArea` gains four optional constraint fields:

```go
type UIArea struct {
    ID        string          `json:"id"`
    Placement UIAreaPlacement `json:"placement"`
    Weight    int             `json:"weight,omitempty"`

    // Height constraints. "" / 0 means unconstrained.
    // Values: "3" (absolute lines) or "20%" (percent of terminal height).
    // Rendered output is padded up to MinHeight and truncated to MaxHeight.
    MinHeight string `json:"min_height,omitempty"`
    MaxHeight string `json:"max_height,omitempty"`

    // Width constraints. "" means full available width.
    // Values: "80" (absolute cols) or "50%" (percent of terminal width).
    MinWidth string `json:"min_width,omitempty"`
    MaxWidth string `json:"max_width,omitempty"`
}
```

### New host call: `ui_update_area`

Extensions can update constraints on an existing area at any time:

```
Method: "ui_update_area"
Permission: PermUI
Params: UIUpdateAreaParams
```

```go
type UIUpdateAreaParams struct {
    ID        string `json:"id"`
    MinHeight string `json:"min_height,omitempty"`
    MaxHeight string `json:"max_height,omitempty"`
    MinWidth  string `json:"min_width,omitempty"`
    MaxWidth  string `json:"max_width,omitempty"`
    Weight    *int   `json:"weight,omitempty"` // nil = leave unchanged
}
```

All fields are optional — omitted fields leave the current value unchanged. Errors if the
area ID does not exist.

### Constraint resolution (harness)

At render time, for each area:

1. Resolve `MinWidth`/`MaxWidth` against `m.width` → clamp the width passed to `Render`.
2. Call `scene.Render(id, clampedWidth)` → raw string.
3. Count lines in raw string.
4. Resolve `MinHeight`/`MaxHeight` against `m.height` → `[minLines, maxLines]`.
5. Pad with blank lines if below `minLines`; truncate if above `maxLines`.

Percentage resolution: `"20%"` of terminal height `h` → `h * 20 / 100`. Absolute values
are parsed as decimal integers. Empty strings skip the constraint.

---

## SceneRenderer Changes

`sceneArea` gains the four constraint fields mirroring `UIArea`:

```go
type sceneArea struct {
    root      *sdk.UINode
    id        string
    placement sdk.UIAreaPlacement
    weight    int
    minHeight string
    maxHeight string
    minWidth  string
    maxWidth  string
}
```

New methods on `SceneRenderer`:

```go
// UpdateArea applies a UIUpdateAreaParams to an existing area.
// Returns an error if the ID does not exist.
func (s *SceneRenderer) UpdateArea(p sdk.UIUpdateAreaParams) error

// ConstrainWidth returns the render width for an area after applying MinWidth/MaxWidth.
func (s *SceneRenderer) ConstrainWidth(id string, termWidth int) int

// ConstrainHeight returns the clamped line count for an area after applying MinHeight/MaxHeight.
func (s *SceneRenderer) ConstrainHeight(id string, lines int, termHeight int) int
```

---

## Harness Layout Changes

### `statuslineAreaID` constant

```go
const statuslineAreaID = "statusline"
```

The harness creates the `statusline` area in `SceneRenderer` at startup (in `New()`) so
the placement slot is reserved before `session_start` fires and the WASM extension runs.
It creates it empty — the bundled extension sets the root node tree on `session_start`.

```go
// In New(): reserve the statusline placement.
_ = m.scene.CreateArea(sdk.UIArea{
    ID:        statuslineAreaID,
    Placement: sdk.UIAreaStatus,
})
```

### `statusLineHeight() int`

```go
func (m Model) statusLineHeight() int {
    total := 0
    for _, id := range m.scene.AreasByPlacement(sdk.UIAreaStatus) {
        width := m.scene.ConstrainWidth(id, m.width)
        rendered := m.scene.Render(id, width)
        lines := strings.Count(rendered, "\n") + 1
        if rendered == "" {
            lines = 0
        }
        lines = m.scene.ConstrainHeight(id, lines, m.height)
        total += lines
    }
    return total
}
```

### `chatHeight()` update

```go
func (m Model) chatHeight() int {
    h := m.height - inputAreaHeight - m.statusLineHeight() - m.dropdownHeight() - m.consoleHeight()
    if h < 1 {
        h = 1
    }
    return h
}
```

`statusBarHeight = 0` constant is removed.

### `renderStatusLine() string`

```go
func (m Model) renderStatusLine() string {
    var parts []string
    for _, id := range m.scene.AreasByPlacement(sdk.UIAreaStatus) {
        width := m.scene.ConstrainWidth(id, m.width)
        rendered := strings.TrimRight(m.scene.Render(id, width), "\n")
        if rendered == "" {
            continue
        }
        // Clamp lines to MaxHeight constraint.
        rawLines := strings.Split(rendered, "\n")
        clamped := m.scene.ConstrainHeight(id, len(rawLines), m.height)
        if clamped < len(rawLines) {
            rawLines = rawLines[:clamped]
        }
        parts = append(parts, strings.Join(rawLines, "\n"))
    }
    if len(parts) == 0 {
        return ""
    }
    return strings.Join(parts, "\n")
}
```

### `View()` update

The statusline block is inserted between the chat/console/dropdown and the input box:

```go
if sl := m.renderStatusLine(); sl != "" {
    sb.WriteString(sl + "\n")
}
sb.WriteString(inputBox)
```

### `renderInputBox()` top border

The status text is removed from the input box top border. It becomes a plain ruled line:

```
╭──────────────────────────────────────────╮
│ cursor line                              │
╰──────────────────────────────────────────╯
```

---

## Removals

| Removed | Replaced by |
|---------|-------------|
| `statusbar.go` (`StatusBar` struct) | `statusline` scene area |
| `StatusUpdateMsg{Key, Value}` | `ui_patch` on `"statusline"` area |
| `StatusBar.Line()` / `StatusBar.View()` | `scene.Render("statusline", width)` |
| `StatusBar.statuses` map | nodes in the statusline scene tree |
| `statusBarHeight = 0` constant | `statusLineHeight()` dynamic method |
| Status text in `renderInputBox` top border | plain `╭──────────────╮` |
| `SetStatus(key, value)` in WASM SDK | `UIPatch` on `"statusline"` |
| `SetStatusLine(text)` in WASM SDK | `UIPatch` with `set_root` or `update` op |

`liveState` keeps its fields (`streaming`, `tokens`, `model`, `provider`) — they feed the
`get_status_info` host call which the `statusline` extension uses to read harness state.
`get_status_info` is **not** removed; it becomes the primary data source for the statusline
extension.

`StatusUpdateMsg` is removed from the internal message table. The `/status` built-in
command is removed or repurposed.

---

## Bundled `statusline` Extension

The existing `extensions/statusline/` extension is rewritten to use the scene graph instead
of `SetStatusLine`. It becomes the canonical owner of the `"statusline"` area.

### Default node tree

On `session_start`, after calling `ui_create_area` (which will now be a no-op since the
harness pre-creates the area — the extension calls `ui_patch` with `set_root` instead):

```
statusline-root  (hstack)
  ├── sl-provider   (text, fg:"muted")     e.g. "anthropic"
  ├── sl-sep-1      (text)                 "  "
  ├── sl-model      (text)                 e.g. "claude-sonnet-4-5"
  ├── sl-sep-2      (text)                 "  "
  ├── sl-tokens     (text, fg:"muted")     e.g. "tokens:1.2k"
  └── sl-working    (text, fg:"accent")    "" (empty until streaming)
```

### Event subscriptions

| Event | Action |
|-------|--------|
| `session_start` | `ui_patch` set_root with default tree |
| `EventTick` (1s) | `get_status_info` → patch `sl-provider`, `sl-model`, `sl-tokens` if changed |
| `EventToken` | patch `sl-working` with animated indicator + elapsed time |
| `EventAfterProviderResponse` | clear `sl-working`, update `sl-tokens` with final count |
| `EventContextUsage` | insert/update `sl-ctx` node (e.g. `"ctx rem: 42%"`) |

### Extension injection point

Other extensions that want to add segments insert nodes into `"statusline-root"` using
`ui_patch` with an `insert` op. The stable node IDs (`sl-provider`, `sl-model`, etc.) let
the default extension continue patching its own nodes without clobbering injected ones.

Example — a sub-agent monitor extension adding an `sl-agents` node:

```json
{
  "area": "statusline",
  "ops": [
    {
      "op": "insert",
      "parent": "statusline-root",
      "node": { "id": "sl-agents", "type": "text", "text": "  agents:2" }
    }
  ]
}
```

---

## ABI Changes (`docs/extensions.md`)

The following changes must be reflected in `docs/extensions.md`:

### New host calls

| Method | Params | Permission | Description |
|--------|--------|-----------|-------------|
| `ui_update_area` | `UIUpdateAreaParams` | `PermUI` | Update constraints on an existing area |

### Changed types

- `UIArea` gains `min_height`, `max_height`, `min_width`, `max_width` fields (all optional strings)

### New placement constant

- `UIAreaInput = "input"` — logical slot for the input box (always harness-owned; listed for documentation)

### Deprecated / removed host calls

| Method | Replacement |
|--------|-------------|
| `set_status` (via `StatusUpdateMsg`) | `ui_patch` on `"statusline"` area |
| `set_status_line` (via `SetStatusLine`) | `ui_patch` with `set_root` op on `"statusline"` area |

### WASM SDK (`wllrsdk.go`) changes

- `SetStatus(key, value string)` — **removed**
- `SetStatusLine(text string)` — **removed**
- `UICreateArea(area UIArea)` — gains `MinHeight`, `MaxHeight`, `MinWidth`, `MaxWidth` fields
- `UIUpdateArea(params UIUpdateAreaParams)` — **new**

---

## Spec / Notes Updates Required

| Module | File | Change |
|--------|------|--------|
| `modules/harness` | `SPECS.md §18` | Replace StatusBar spec with statusline scene area spec |
| `modules/harness` | `SPECS.md §22` | Update View Layout diagram |
| `modules/harness` | `NOTES.md` | New entry: statusline moved to scene graph |
| `modules/harness` | `TESTS.md` | New tests: statusline area lifecycle, constraint resolution, multi-extension injection |
| `modules/sdk` | `SPECS.md` | Document `UIUpdateAreaParams`, `UIArea` constraint fields, `UIAreaInput` |
| `modules/sdk` | `NOTES.md` | New entry: area sizing constraints added |
| `docs/extensions.md` | — | Document `ui_update_area`, updated `UIArea`, removed `set_status`/`set_status_line` |

---

## Implementation Order

1. **`modules/sdk`** — add `UIUpdateAreaParams`, extend `UIArea` with constraint fields, add `UIAreaInput`, add `MethodUIUpdateArea` constant
2. **`modules/harness` SceneRenderer** — add `UpdateArea`, `ConstrainWidth`, `ConstrainHeight` methods; grow `sceneArea` struct
3. **`modules/extension` host** — wire `ui_update_area` host call to `SceneRenderer.UpdateArea`
4. **`modules/harness` Model** — pre-create `statusline` area in `New()`; add `statusLineHeight()`, `renderStatusLine()`; update `chatHeight()` and `View()`; strip status from `renderInputBox()`; remove `StatusBar` and `StatusUpdateMsg`
5. **`extensions/statusline`** — rewrite to use `ui_patch` scene API; subscribe to `EventToken`, `EventAfterProviderResponse`, `EventContextUsage`
6. **`extensions/wllrsdk.go`** — remove `SetStatus`/`SetStatusLine`; add `UIUpdateArea`; update `UICreateArea` params
7. **Spec / Notes / docs** — update all affected spec files and `docs/extensions.md`
