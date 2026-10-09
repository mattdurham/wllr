package server

// NOTE: Any changes to this file must be reflected in the corresponding SPECS.md or NOTES.md.

import (
	"fmt"
	"sync"

	"github.com/mattdurham/wllr/modules/sdk"
)

// SceneModel is the TUI-free holder of the extension UI scene graph for serve
// mode. It mirrors the *state* half of the harness SceneRenderer: it owns the
// areas and their node trees, applies patches, and answers layout queries — but
// it renders to HTML (htmlscene.go) rather than to a terminal.
//
// It deliberately does not import modules/harness: the patch semantics it needs
// (insert/remove/append/replace) live in modules/sdk (uigraph.go), which both
// renderers share, so the graph is manipulated identically and only the final
// node-to-pixels step differs. That is what lets a browser see the same scene
// the TUI would have drawn.
//
// All methods are safe for concurrent use: UISink scene calls arrive from
// concurrent WASM dispatch goroutines while the SSE handler reads snapshots.
type SceneModel struct {
	areas map[string]*sceneAreaState
	order []string // area IDs in creation order, for deterministic compositing
	mu    sync.RWMutex
}

// sceneAreaState is one named region owned by an extension.
type sceneAreaState struct {
	root *sdk.UINode
	area sdk.UIArea
}

// NewSceneModel returns an empty SceneModel.
func NewSceneModel() *SceneModel {
	return &SceneModel{areas: map[string]*sceneAreaState{}}
}

// CreateArea registers a new area. It returns an error if the ID is empty or
// already exists, matching the harness renderer's contract so an extension sees
// identical behavior on either front end.
func (m *SceneModel) CreateArea(a sdk.UIArea) error {
	if a.ID == "" {
		return fmt.Errorf("ui_create_area: area id is required")
	}
	m.mu.Lock()
	defer m.mu.Unlock()
	if _, ok := m.areas[a.ID]; ok {
		return fmt.Errorf("ui_create_area: area already exists: %s", a.ID)
	}
	m.areas[a.ID] = &sceneAreaState{area: a}
	m.order = append(m.order, a.ID)
	return nil
}

// RemoveArea deletes an area and its scene graph. Removing a missing area is a
// no-op.
func (m *SceneModel) RemoveArea(id string) {
	m.mu.Lock()
	defer m.mu.Unlock()
	if _, ok := m.areas[id]; !ok {
		return
	}
	delete(m.areas, id)
	out := m.order[:0]
	for _, x := range m.order {
		if x != id {
			out = append(out, x)
		}
	}
	m.order = out
}

// UpdateArea applies a UIUpdateAreaParams, replacing only the non-empty fields.
// It errors on an unknown ID.
func (m *SceneModel) UpdateArea(p sdk.UIUpdateAreaParams) error {
	if p.ID == "" {
		return fmt.Errorf("ui_update_area: area id is required")
	}
	m.mu.Lock()
	defer m.mu.Unlock()
	st, ok := m.areas[p.ID]
	if !ok {
		return fmt.Errorf("ui_update_area: area not found: %s", p.ID)
	}
	if p.MinHeight != "" {
		st.area.MinHeight = p.MinHeight
	}
	if p.MaxHeight != "" {
		st.area.MaxHeight = p.MaxHeight
	}
	if p.MinWidth != "" {
		st.area.MinWidth = p.MinWidth
	}
	if p.MaxWidth != "" {
		st.area.MaxWidth = p.MaxWidth
	}
	if p.Weight != nil {
		st.area.Weight = *p.Weight
	}
	return nil
}

// ApplyPatch applies a batch of ops to an area atomically, using the shared
// sdk.ApplyOp semantics. The batch is validated against a working clone; if any
// op fails, the whole batch is rejected and the live tree is unchanged.
func (m *SceneModel) ApplyPatch(p sdk.UIPatchParams) error {
	m.mu.Lock()
	defer m.mu.Unlock()
	st, ok := m.areas[p.Area]
	if !ok {
		return fmt.Errorf("ui_patch: unknown area: %s", p.Area)
	}
	working := sdk.CloneNode(st.root)
	for i, op := range p.Ops {
		next, err := sdk.ApplyOp(working, op)
		if err != nil {
			return fmt.Errorf("ui_patch op %d (%s): %w", i, op.Op, err)
		}
		working = next
	}
	st.root = working
	return nil
}

// HasArea reports whether an area exists.
func (m *SceneModel) HasArea(id string) bool {
	m.mu.RLock()
	defer m.mu.RUnlock()
	_, ok := m.areas[id]
	return ok
}

// Len returns the number of registered areas.
func (m *SceneModel) Len() int {
	m.mu.RLock()
	defer m.mu.RUnlock()
	return len(m.areas)
}

// Snapshot returns every area in creation order with its current HTML. It is
// what a freshly connected client fetches to seed the scene before applying
// live patches, so a client that missed the creation frames is still correct.
func (m *SceneModel) Snapshot() []AreaView {
	m.mu.RLock()
	defer m.mu.RUnlock()
	out := make([]AreaView, 0, len(m.order))
	for _, id := range m.order {
		st := m.areas[id]
		out = append(out, AreaView{Area: st.area, HTML: RenderAreaHTML(st.area, st.root)})
	}
	return out
}

// AreaHTML returns the rendered HTML for one area.
func (m *SceneModel) AreaHTML(id string) (string, bool) {
	m.mu.RLock()
	defer m.mu.RUnlock()
	st, ok := m.areas[id]
	if !ok {
		return "", false
	}
	return RenderAreaHTML(st.area, st.root), true
}

// IsTextAppendOnly reports whether a batch consists solely of append_text ops,
// which the front end can apply as an incremental text delta instead of
// re-rendering the whole area. This mirrors the terminal renderer's append fast
// path (RenderAppendTextNode); an empty batch is not an append.
func IsTextAppendOnly(ops []sdk.UIPatchOp) bool {
	if len(ops) == 0 {
		return false
	}
	for _, op := range ops {
		if op.Op != sdk.UIOpAppendText {
			return false
		}
	}
	return true
}
