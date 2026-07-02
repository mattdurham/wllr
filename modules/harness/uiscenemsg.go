package harness

// NOTE: Any changes to this file must be reflected in the corresponding SPECS.md or NOTES.md.

// sceneDirtyMsg is sent by the UI bridge after it mutates the shared
// SceneRenderer (create/patch/remove area), primarily to trigger a re-render of
// the bubbletea View. The scene state itself is mutated synchronously by the
// bridge (the SceneRenderer is goroutine-safe). Area identifies the mutated area
// so the Model can avoid expensive chat viewport refreshes for unrelated areas
// such as the statusline.
type sceneDirtyMsg struct {
	Area       string
	AppendID   string
	AppendText string
	AppendOnly bool
}

type chatAppendRefreshMsg struct{}
