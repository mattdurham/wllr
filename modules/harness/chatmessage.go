package harness

import "github.com/mattdurham/wllr/modules/sdk"

// chatMessage is a finalised message in the chat history.
type chatMessage struct {
	role   sdk.Role
	content string
	queued bool // true when message was submitted while the agent was mid-turn
}
