package harness

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
