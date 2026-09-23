package agent_test

import (
	"sync"
	"testing"
	"time"

	"github.com/mattdurham/wllr/modules/agent"
	"github.com/mattdurham/wllr/modules/sdk"
	"github.com/mattdurham/wllr/modules/testutil"
)

// Submit is reachable from a host call (agent_deliver, agent_run,
// agent_send_message), so its stack may already be inside an extension's WASM
// call. A turn-start callback that dispatches back into that extension would
// then wait on a mutex the caller holds and cannot release.
//
// The invariant: Submit must not run the turn-start callback on its caller's
// stack. Modeled here with a lock the caller holds and the callback needs.
//
// This is a liveness assertion, not a race assertion: a deadlock is correctly
// synchronized code that never completes, so -race cannot see it.
func TestSubmitDoesNotRunTurnStartOnCallerStack(t *testing.T) {
	pool := agent.NewPool()
	prov := testutil.NewFakeProvider("ok")
	pool.SetProvider(prov)
	pool.SetDefaultModelName("fake-model")
	pool.SetModelContextWindow("fake-model", 100000)
	lm, err := pool.LanguageModelForModel(t.Context(), "fake-model")
	if err != nil {
		t.Fatal(err)
	}
	a, err := pool.Spawn("main", lm, agent.SpawnOpts{ModelName: "fake-model", ContextWindow: 100000})
	if err != nil {
		t.Fatal(err)
	}

	// Stands in for the extension host's per-extension call mutex, held by the
	// outer host call whose stack Submit is running on.
	var callMu sync.Mutex
	callMu.Lock()
	reached := make(chan struct{})
	a.SetOnTurnStart(func(string, []sdk.Message) {
		callMu.Lock() // blocks until the "host call" returns
		defer callMu.Unlock()
		close(reached)
	})

	done := make(chan struct{})
	go func() {
		defer close(done)
		_ = pool.Deliver("main", sdk.Message{Role: sdk.RoleUser, Content: "hi"}, true)
	}()

	select {
	case <-done:
		// Good: Deliver returned without waiting for the callback.
	case <-time.After(3 * time.Second):
		t.Fatal("Deliver blocked on the turn-start callback (callback ran on the caller's stack)")
	}

	// Releasing models the outer host call returning; the callback then runs.
	callMu.Unlock()
	select {
	case <-reached:
	case <-time.After(3 * time.Second):
		t.Fatal("turn-start callback never ran after the lock was released")
	}
}
