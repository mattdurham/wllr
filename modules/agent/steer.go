package agent

// NOTE: Any changes to this file must be reflected in the corresponding SPECS.md or NOTES.md.

import (
	"context"

	"charm.land/fantasy"

	"github.com/mattdurham/wllr/modules/sdk"
)

// steerInjector delivers mid-turn steering: steer-typed inbox messages are
// pulled into the running turn at step boundaries instead of waiting for the
// next turn. fantasy's step loop rebuilds each step's request from the fixed
// initial prompt plus accumulated step messages, so an injected message would
// vanish from the NEXT request unless prepare re-appends it every step — the
// injector therefore accumulates everything it has delivered and re-appends
// the full set on every subsequent prepare. Without a running turn the same
// messages flow through Submit's normal drain path (steer is model-visible,
// unlike system/steering), so no separate idle path exists.
//
// Lifecycle: one injector per turn, created in streamTurn. Both the proactive
// stream and the reactive compaction retry share one instance so messages
// delivered during the failed attempt are not lost from the retry.
type steerInjector struct {
	a       *Agent
	onSteer func(content string)
	// injected holds every steer message delivered during this turn, in
	// delivery order. Read by executeTurn after the turn to record them into
	// history and the canonical transcript.
	injected []sdk.Message
}

// newSteerInjector captures the agent's steer callback once (the callback may
// be re-registered between turns; one turn must use one callback).
func newSteerInjector(a *Agent) *steerInjector {
	a.onSteerMu.RLock()
	fn := a.onSteerFn
	a.onSteerMu.RUnlock()
	return &steerInjector{a: a, onSteer: fn}
}

// wrap composes the injector with the tool-loop compactor's PrepareStep: the
// compactor runs first (it may compact the request), then any steer messages
// are appended after the compacted payload so guidance survives compaction.
// When nothing has been delivered the compactor's result passes through
// unchanged, preserving the exact default request shape.
func (si *steerInjector) wrap(compactor fantasy.PrepareStepFunction) fantasy.PrepareStepFunction {
	if si == nil {
		return compactor
	}
	return func(ctx context.Context, opts fantasy.PrepareStepFunctionOptions) (context.Context, fantasy.PrepareStepResult, error) {
		si.drain()
		result := fantasy.PrepareStepResult{}
		if compactor != nil {
			var err error
			ctx, result, err = compactor(ctx, opts)
			if err != nil {
				return ctx, fantasy.PrepareStepResult{}, err
			}
		}
		if len(si.injected) == 0 {
			return ctx, result, nil
		}
		base := opts.Messages
		if result.Messages != nil {
			base = result.Messages
		}
		// Copy before appending: base is owned by fantasy (it reuses the
		// backing array across steps via append), so mutating it in place
		// would corrupt the next step's input.
		out := make([]fantasy.Message, 0, len(base)+len(si.injected))
		out = append(out, base...)
		for _, m := range si.injected {
			out = append(out, fantasy.Message{
				Role:    fantasy.MessageRoleUser,
				Content: []fantasy.MessagePart{fantasy.TextPart{Text: m.Content}},
			})
		}
		result.Messages = out
		return ctx, result, nil
	}
}

// drain pulls newly-arrived steer messages out of the inbox and fires the
// render callback for each. Only steer-typed messages are taken — the inbox
// may simultaneously hold system control messages (shutdown_request) and
// normal queued messages that must wait for finishTurn's post-turn drain.
func (si *steerInjector) drain() {
	msgs := si.a.inbox.drainSteer()
	if len(msgs) == 0 {
		return
	}
	si.injected = append(si.injected, msgs...)
	for _, m := range msgs {
		if si.onSteer != nil {
			si.onSteer(m.Content)
		}
	}
}

// SetOnSteer sets the callback invoked on the turn goroutine when a steer
// message is delivered into the running turn (injected into the next provider
// request). Used by the harness to render the steered text in the transcript
// at delivery time. Thread-safe; may be called before each Submit.
func (a *Agent) SetOnSteer(fn func(content string)) {
	a.onSteerMu.Lock()
	a.onSteerFn = fn
	a.onSteerMu.Unlock()
}
