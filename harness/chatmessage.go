package harness

import "github.com/mattdurham/wllr/sdk"

// chatMessage is a finalised message in the chat history.
type chatMessage struct {
	role    sdk.Role
	content string
}
