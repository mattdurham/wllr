package sdk

// NOTE: Any changes to this file must be reflected in the corresponding SPECS.md or NOTES.md.

// UIPatchParams is the params blob for the ui_patch host_call. Ops apply in
// order to the named Area; the batch is all-or-nothing — if any op references a
// missing node the host rejects the whole batch with an error response.
type UIPatchParams struct {
	Area string      `json:"area"`
	Ops  []UIPatchOp `json:"ops"`
}
