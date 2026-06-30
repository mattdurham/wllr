package sdk

// NOTE: Any changes to this file must be reflected in the corresponding SPECS.md or NOTES.md.

// NotifyPayload is the payload for EventNotify. Text is the notification line as
// it would appear in the chat (it may begin with "⚠" for warnings/errors).
type NotifyPayload struct {
	Text string `json:"text"`
}
