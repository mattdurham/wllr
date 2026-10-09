package server

// NOTE: Any changes to this file must be reflected in the corresponding SPECS.md or NOTES.md.

import "github.com/mattdurham/wllr/modules/sdk"

// AreaView is one area's metadata plus its rendered HTML, as handed to a
// client by a snapshot.
type AreaView struct {
	HTML string     `json:"html"`
	Area sdk.UIArea `json:"area"`
}
