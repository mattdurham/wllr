package harness

// NOTE: Any changes to this file must be reflected in the corresponding SPECS.md or NOTES.md.

import (
	tea "charm.land/bubbletea/v2"
	"strings"
	"sync"
	"time"
)

type tokenBatcher struct {
	lastSend time.Time
	p        *tea.Program
	buf      strings.Builder
	mu       sync.Mutex
}
