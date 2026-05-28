package agent

// pool_internal_test.go contains tests that need access to unexported symbols
// (e.g. addTokens). Tests that only need the public API belong in pool_test.go.

import "testing"

func TestAgentPool_TokenCount(t *testing.T) {
	pool := NewPool()
	if pool.TokenCount() != 0 {
		t.Errorf("initial TokenCount: got %d, want 0", pool.TokenCount())
	}
	// Increment via the internal method (package-private; not exposed to external callers).
	pool.addTokens(5)
	if pool.TokenCount() != 5 {
		t.Errorf("TokenCount after addTokens(5): got %d, want 5", pool.TokenCount())
	}
	pool.addTokens(3)
	if pool.TokenCount() != 8 {
		t.Errorf("TokenCount after addTokens(3): got %d, want 8", pool.TokenCount())
	}
}
