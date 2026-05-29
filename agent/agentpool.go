package agent

import (
	"sync"
	"sync/atomic"

	"charm.land/fantasy"
)

// AgentPool manages all live agents and a shared token counter.
// It is safe for concurrent use from multiple goroutines.
type AgentPool struct {
	provider           fantasy.Provider
	agents             map[string]*Agent
	teams              map[string]*Team
	providerName       string
	defaultModelName   string
	baseSystemPrompt   string
	contextWindow      int64
	tokenCount         atomic.Int64
	mu                 sync.RWMutex
	baseSystemPromptMu sync.RWMutex
}
