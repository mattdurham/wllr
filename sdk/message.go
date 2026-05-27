package sdk

// Message is a chat message.
type Message struct {
	Role    Role   `json:"role"`
	Content string `json:"content"`
}
