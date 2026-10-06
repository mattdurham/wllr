package extension

import (
	"context"
	"testing"
	"time"

	"github.com/mattdurham/wllr/modules/sdk"
)

// initBlockingWASM is a hand-encoded WASM module for the init/dispatch race
// regression test. It is like minimalWASM except:
//   - imports env.host_call(i32,i32,i32,i32) i32
//   - _init performs one host_call for the request JSON stored at memory[16]
//     ({"method":"init_block"}), passing response slots at 4 and 8, then
//     returns 0.
//   - _alloc returns the fixed pointer 64 (the test never reads what the host
//     writes there).
var initBlockingWASM = []byte{
	// magic + version
	0x00, 0x61, 0x73, 0x6D, 0x01, 0x00, 0x00, 0x00,

	// type section: 5 types
	0x01, 0x1C, // id=1, size=28
	0x05,                                           // count=5
	0x60, 0x04, 0x7F, 0x7F, 0x7F, 0x7F, 0x01, 0x7F, // type 0: (i32,i32,i32,i32)->i32 (host_call)
	0x60, 0x00, 0x01, 0x7F, // type 1: ()->i32 (_init)
	0x60, 0x02, 0x7F, 0x7F, 0x01, 0x7F, // type 2: (i32,i32)->i32 (_on_event)
	0x60, 0x01, 0x7F, 0x01, 0x7F, // type 3: (i32)->i32 (_alloc)
	0x60, 0x01, 0x7F, 0x00, // type 4: (i32)->() (_free)

	// import section: env.host_call : func type 0
	0x02, 0x11, // id=2, size=17
	0x01,                   // count=1
	0x03, 0x65, 0x6E, 0x76, // module "env"
	0x09, 0x68, 0x6F, 0x73, 0x74, 0x5F, 0x63, 0x61, 0x6C, 0x6C, // name "host_call"
	0x00, 0x00, // kind=func, type index 0

	// function section: 4 locally-defined functions
	0x03, 0x05, // id=3, size=5
	0x04,                   // count=4
	0x01, 0x02, 0x03, 0x04, // type indices (funcs 1..4; func 0 is the import)

	// memory section: 1 page
	0x05, 0x03, // id=5, size=3
	0x01,       // count=1
	0x00, 0x01, // min=1, no max

	// export section
	0x07, 0x2F, // id=7, size=47
	0x05,                                                 // count=5
	0x06, 0x6D, 0x65, 0x6D, 0x6F, 0x72, 0x79, 0x02, 0x00, // "memory" memory 0
	0x05, 0x5F, 0x69, 0x6E, 0x69, 0x74, 0x00, 0x01, // "_init" func 1
	0x09, 0x5F, 0x6F, 0x6E, 0x5F, 0x65, 0x76, 0x65, 0x6E, 0x74, 0x00, 0x02, // "_on_event" func 2
	0x06, 0x5F, 0x61, 0x6C, 0x6C, 0x6F, 0x63, 0x00, 0x03, // "_alloc" func 3
	0x05, 0x5F, 0x66, 0x72, 0x65, 0x65, 0x00, 0x04, // "_free" func 4

	// code section
	0x0A, 0x1F, // id=10, size=31
	0x04, // count=4
	// body 1 (_init), size=15: host_call(16, 23, 4, 8); drop; i32.const 0
	0x0F, 0x00,
	0x41, 0x10, // i32.const 16 (reqPtr)
	0x41, 0x17, // i32.const 23 (reqLen)
	0x41, 0x04, // i32.const 4 (respPtrPtr)
	0x41, 0x08, // i32.const 8 (respLenPtr)
	0x10, 0x00, // call 0 (host_call)
	0x1A,       // drop
	0x41, 0x00, // i32.const 0
	0x0B, // end
	// body 2 (_on_event), size=4: i32.const 0
	0x04, 0x00, 0x41, 0x00, 0x0B,
	// body 3 (_alloc), size=5: i32.const 64
	0x05, 0x00, 0x41, 0xC0, 0x00, 0x0B,
	// body 4 (_free), size=2: end
	0x02, 0x00, 0x0B,

	// data section: memory[16] = `{"method":"init_block"}` (23 bytes)
	0x0B, 0x1D, // id=11, size=29
	0x01,             // count=1
	0x00,             // flags: active, memory 0
	0x41, 0x10, 0x0B, // offset expr: i32.const 16, end
	0x17,                                                       // byte count=23
	0x7B, 0x22, 0x6D, 0x65, 0x74, 0x68, 0x6F, 0x64, 0x22, 0x3A, // {"method":
	0x22, 0x69, 0x6E, 0x69, 0x74, 0x5F, 0x62, 0x6C, 0x6F, 0x63, 0x6B, 0x22, // "init_block"
	0x7D, // }
}

// TestLoad_InitSerializedWithDispatch is the regression test for the logging
// extension's startup trap ("host_call: _alloc failed ... unreachable" in
// TinyGo's asyncify machinery). During load, _init runs host_calls whose
// responses re-enter the module via _alloc; a concurrent dispatcher (the log
// drain goroutine flushing buffered records the moment EventLog gains a
// subscriber) used to invoke _alloc/_on_event on the same module at the same
// time, corrupting the asyncify state machine. The fix holds ext.callMu
// across callInit, so a dispatch attempted mid-init must block until _init
// returns.
func TestLoad_InitSerializedWithDispatch(t *testing.T) {
	h := NewHost(nil)
	t.Cleanup(func() { _ = h.Close(context.Background()) })

	// Test-only host_call method: signals that _init is inside the host call,
	// then blocks until the test releases it. Added after NewHost and before
	// Load, so no concurrent map access.
	entered := make(chan struct{})
	release := make(chan struct{})
	h.dispatch["init_block"] = func(_ context.Context, _ *Extension, _ sdk.HostCallRequest) sdk.HostCallResponse {
		close(entered)
		<-release
		return sdk.HostCallResponse{}
	}

	path := writeWASM(t, "initblock.wasm", initBlockingWASM)

	loadDone := make(chan error, 1)
	go func() { loadDone <- h.Load(context.Background(), path) }()

	// Wait until _init is inside the blocking host call: the extension is
	// registered and callInit is in flight.
	select {
	case <-entered:
	case err := <-loadDone:
		t.Fatalf("load returned before _init reached the blocking host call: %v", err)
	case <-time.After(10 * time.Second):
		t.Fatal("_init did not reach the blocking host call")
	}

	// Subscribe the extension to a test event so DispatchEvent attempts a
	// dispatch into the module.
	raceEvt := sdk.EventType("test_race")
	ext := h.extensions[len(h.extensions)-1]
	ext.subMu.Lock()
	ext.subscriptions[raceEvt] = true
	ext.subMu.Unlock()

	dispDone := make(chan error, 1)
	go func() {
		_, err := h.DispatchEvent(context.Background(), sdk.Event{Type: raceEvt, Payload: []byte(`{}`)})
		dispDone <- err
	}()

	// While _init is blocked, the dispatch must not complete: it has to wait
	// on ext.callMu. Pre-fix it would invoke _alloc/_on_event concurrently and
	// return (or error) immediately.
	select {
	case err := <-dispDone:
		t.Fatalf("dispatch completed during in-flight _init (err=%v): exports invoked concurrently with init", err)
	case <-time.After(300 * time.Millisecond):
	}

	// Let _init finish; load and the queued dispatch must then complete.
	close(release)
	if err := <-loadDone; err != nil {
		t.Fatalf("load failed: %v", err)
	}
	select {
	case err := <-dispDone:
		if err != nil {
			t.Fatalf("dispatch failed after init completed: %v", err)
		}
	case <-time.After(10 * time.Second):
		t.Fatal("dispatch did not complete after init finished")
	}
}
