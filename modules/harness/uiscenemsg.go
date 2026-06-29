package harness

// NOTE: Any changes to this file must be reflected in the corresponding SPECS.md or NOTES.md.

// sceneDirtyMsg is sent by the UI bridge after it mutates the shared
// SceneRenderer (create/patch/remove area), purely to trigger a re-render of
// the bubbletea View. The scene state itself is mutated synchronously by the
// bridge (the SceneRenderer is goroutine-safe), so this message carries no
// payload and the Model handles it as a no-op that forces a redraw.
type sceneDirtyMsg struct{}
