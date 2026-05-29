package extension

import "github.com/mattdurham/wllr/modules/sdk"

// RegisteredToolInfo pairs a registered tool with the name of the extension
// that registered it.  OwnerName is empty for tools registered outside of an
// extension context.
type RegisteredToolInfo struct {
	OwnerName string
	Tool      sdk.Tool
}
