package client

// NOTE: Any changes to this file must be reflected in the corresponding SPECS.md or NOTES.md.

import (
	"context"
	"errors"
	"fmt"
	"sort"
	"sync"
)

// Multiplexer connects to N wllr instances simultaneously and merges their
// event streams into one channel, tagging every event with its source instance.
//
// It is deliberately out-of-process: each instance is a separate wllr server
// (separate config, session dir, and extension host). Merging happens here, in
// the client, rather than by co-hosting sessions in one process, because the
// engine has process-global singletons (one extension host, one config file,
// one session directory) that do not co-exist.
type Multiplexer struct {
	events chan Tagged

	cancel  context.CancelFunc
	done    chan struct{}
	clients []*Client

	opts FollowOptions

	mu      sync.Mutex
	running bool
}

// DefaultMergeBuffer is the default merged-channel capacity.
const DefaultMergeBuffer = 1024

// NewMultiplexer builds a multiplexer for the given instance configs. Each must
// have a unique, non-empty ID; a duplicate is an error, since the ID is the only
// thing that disambiguates identically named agents across instances.
func NewMultiplexer(configs []Config, opts MultiplexerOptions) (*Multiplexer, error) {
	if len(configs) == 0 {
		return nil, fmt.Errorf("client: at least one instance is required")
	}
	seen := make(map[string]bool, len(configs))
	clients := make([]*Client, 0, len(configs))
	for _, cfg := range configs {
		c, err := New(cfg)
		if err != nil {
			return nil, err
		}
		if seen[c.id] {
			return nil, fmt.Errorf("client: duplicate instance ID %q", c.id)
		}
		seen[c.id] = true
		clients = append(clients, c)
	}
	buf := opts.Buffer
	if buf <= 0 {
		buf = DefaultMergeBuffer
	}
	return &Multiplexer{
		clients: clients,
		opts:    opts.Follow,
		events:  make(chan Tagged, buf),
		done:    make(chan struct{}),
	}, nil
}

// Clients returns the per-instance clients, so a caller can make targeted REST
// calls (submit to one instance) alongside the merged stream.
func (m *Multiplexer) Clients() []*Client { return m.clients }

// Client returns the client for an instance ID, or nil.
func (m *Multiplexer) Client(id string) *Client {
	for _, c := range m.clients {
		if c.id == id {
			return c
		}
	}
	return nil
}

// Events returns the merged, instance-tagged stream. The channel is closed when
// Run returns (every follower stopped).
func (m *Multiplexer) Events() <-chan Tagged { return m.events }

// Run follows every instance concurrently, merging events into Events(), and
// blocks until ctx is cancelled or a follower stops on an OnConnect rejection
// (a version gate). It closes Events() before returning. Run returns a non-nil
// error only for such a rejection; a cancellation returns nil.
//
// Run must be called at most once per multiplexer.
func (m *Multiplexer) Run(ctx context.Context) error {
	m.mu.Lock()
	if m.running {
		m.mu.Unlock()
		return fmt.Errorf("client: multiplexer already running")
	}
	m.running = true
	ctx, cancel := context.WithCancel(ctx)
	m.cancel = cancel
	m.mu.Unlock()
	defer cancel()
	defer close(m.done)

	var wg sync.WaitGroup
	errCh := make(chan error, len(m.clients))
	for _, c := range m.clients {
		wg.Add(1)
		go func(c *Client) {
			defer wg.Done()
			if err := c.Follow(ctx, func(t Tagged) { m.emit(ctx, t) }, m.opts); err != nil &&
				!errors.Is(err, context.Canceled) {
				// A follower rejection is terminal for the whole multiplexer:
				// cancel the derived run context so the remaining followers unwind
				// and Run returns this error instead of blocking on wg.Wait until
				// an external cancel. errCh is buffered to len(clients), so the
				// send never blocks.
				errCh <- err
				cancel()
			}
		}(c)
	}
	wg.Wait()
	close(m.events)

	select {
	case err := <-errCh:
		return err
	default:
		return nil
	}
}

// emit forwards a tagged event to the merged channel, respecting cancellation
// so a follower never blocks forever on a saturated, unread consumer. It selects
// on ctx.Done() rather than m.done because m.done is closed only after Run's
// wg.Wait returns — which cannot happen while a follower is blocked here.
func (m *Multiplexer) emit(ctx context.Context, t Tagged) {
	select {
	case m.events <- t:
	case <-m.done:
	case <-ctx.Done():
	}
}

// Stop cancels following and unblocks any follower waiting on a saturated
// consumer. It is safe to call before or after Run.
func (m *Multiplexer) Stop() {
	m.mu.Lock()
	cancel := m.cancel
	m.mu.Unlock()
	if cancel != nil {
		cancel()
	}
}

// Instances returns the configured instance IDs in sorted order, for a stable
// display ordering independent of goroutine scheduling.
func (m *Multiplexer) Instances() []string {
	ids := make([]string, 0, len(m.clients))
	for _, c := range m.clients {
		ids = append(ids, c.id)
	}
	sort.Strings(ids)
	return ids
}
