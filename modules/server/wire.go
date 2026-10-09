package server

// NOTE: Any changes to this file must be reflected in the corresponding SPECS.md or NOTES.md.

// The stream envelope was extracted into modules/wire so a client can decode
// frames without importing this package (and, transitively, the agent engine and
// its LLM SDKs). These aliases keep the existing server code — which references
// Event, EventKind, the Kind* constants, ProtocolVersion, and TextInputParams
// unqualified — compiling unchanged, while the definitions live in one place.
// wire is a leaf package: it depends only on sdk.

import "github.com/mattdurham/wllr/modules/wire"

// ProtocolVersion is the API/stream wire version, re-exported from wire.
const ProtocolVersion = wire.ProtocolVersion

// EventKind identifies the type of a streamed Event.
type EventKind = wire.EventKind

const (
	KindReady            = wire.KindReady
	KindUserMessage      = wire.KindUserMessage
	KindToken            = wire.KindToken
	KindTurnStarted      = wire.KindTurnStarted
	KindTurnDone         = wire.KindTurnDone
	KindAssistantMessage = wire.KindAssistantMessage
	KindToolCall         = wire.KindToolCall
	KindToolResult       = wire.KindToolResult
	KindContextUsage     = wire.KindContextUsage
	KindNotify           = wire.KindNotify
	KindAgentSpawned     = wire.KindAgentSpawned
	KindAgentClosed      = wire.KindAgentClosed
	KindError            = wire.KindError

	KindModal        = wire.KindModal
	KindPicker       = wire.KindPicker
	KindAgentTree    = wire.KindAgentTree
	KindFocus        = wire.KindFocus
	KindTextInput    = wire.KindTextInput
	KindAbort        = wire.KindAbort
	KindStatus       = wire.KindStatus
	KindConsole      = wire.KindConsole
	KindConsoleClear = wire.KindConsoleClear
	KindResetHistory = wire.KindResetHistory

	KindUIArea       = wire.KindUIArea
	KindUIPatch      = wire.KindUIPatch
	KindUIAreaUpdate = wire.KindUIAreaUpdate
	KindUIAreaRemove = wire.KindUIAreaRemove
	KindUIAreaHTML   = wire.KindUIAreaHTML
	KindUITextAppend = wire.KindUITextAppend
)
