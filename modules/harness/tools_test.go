package harness_test

// NOTE: Any changes to this file must be reflected in the corresponding SPECS.md or NOTES.md.

// The sdkToolAdapter and related functions have moved to github.com/mattdurham/wllr/tools.
// Coverage for ParseInputSchema, NewSDKToolAdapter, and BuildFantasyTools lives in tools/adapter_test.go.
// This file verifies that tools.BuildFantasyTools works with a nil host (delegated from harness).

import (
	"testing"

	"github.com/mattdurham/wllr/modules/tools"
)

func TestBuildFantasyTools_NilHost(t *testing.T) {
	result := tools.BuildFantasyTools(nil, "agent1", nil)
	if result != nil {
		t.Errorf("expected nil for nil host, got %v", result)
	}
}
