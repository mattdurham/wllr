package agent

// NOTE: Any changes to this file must be reflected in the corresponding SPECS.md or NOTES.md.

import "github.com/mattdurham/wllr/modules/sdk"

// callbackfanout.go — turning the agent's additive callback lists into the
// single callbacks the turn machinery invokes.
//
// The agent's per-turn callbacks (token, done, tool-call, turn-start, steer)
// are lists, not slots: Set* replaces the list, Add* appends. That mirrors the
// pool's observer API (SetUsageObserver/AddUsageObserver) and exists for the
// same reason — two independent consumers must be able to observe the same
// turn. Serve mode subscribes a front-end frame publisher AND an
// extension-dispatch forwarder to the root agent; with single-slot callbacks
// one would silently clobber the other, which is exactly how the browser ended
// up with an empty chat transcript and a tool-less agent.
//
// The chaining helpers below collapse a list into one callback. A single
// callback is returned unwrapped so the common case allocates nothing.

// chainTokenCallbacks returns one callback invoking each of fns in order, or nil
// when fns is empty.
func chainTokenCallbacks(fns []func(token string)) func(string) {
	switch len(fns) {
	case 0:
		return nil
	case 1:
		return fns[0]
	}
	return func(token string) {
		for _, fn := range fns {
			fn(token)
		}
	}
}

// chainDoneCallbacks returns one callback invoking each of fns in order, or nil
// when fns is empty.
func chainDoneCallbacks(fns []func(err error)) func(error) {
	switch len(fns) {
	case 0:
		return nil
	case 1:
		return fns[0]
	}
	return func(err error) {
		for _, fn := range fns {
			fn(err)
		}
	}
}

// chainToolCallCallbacks returns one callback invoking each of fns in order, or
// nil when fns is empty.
func chainToolCallCallbacks(fns []func(id, toolName, input string)) func(string, string, string) {
	switch len(fns) {
	case 0:
		return nil
	case 1:
		return fns[0]
	}
	return func(id, toolName, input string) {
		for _, fn := range fns {
			fn(id, toolName, input)
		}
	}
}

// chainTurnStartCallbacks returns one callback invoking each of fns in order, or
// nil when fns is empty. Callers must pass each observer its own copy of the
// inbox slice: an observer that retains or mutates it must not affect the next.
func chainTurnStartCallbacks(fns []func(content string, inbox []sdk.Message)) func(string, []sdk.Message) {
	switch len(fns) {
	case 0:
		return nil
	case 1:
		return fns[0]
	}
	return func(content string, inbox []sdk.Message) {
		for _, fn := range fns {
			cp := make([]sdk.Message, len(inbox))
			copy(cp, inbox)
			fn(content, cp)
		}
	}
}

// chainSteerCallbacks returns one callback invoking each of fns in order, or nil
// when fns is empty.
func chainSteerCallbacks(fns []func(content string)) func(string) {
	switch len(fns) {
	case 0:
		return nil
	case 1:
		return fns[0]
	}
	return func(content string) {
		for _, fn := range fns {
			fn(content)
		}
	}
}
