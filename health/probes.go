// Package health owns bounded dependency readiness checks. Process liveness and
// application lifecycle remain distinct from the availability of dependencies.
package health

import (
	"context"
	"sync"
	"time"

	"github.com/weiloon1234/Foundry-Go/fault"
	"github.com/weiloon1234/Foundry-Go/internal/callback"
	"github.com/weiloon1234/Foundry-Go/internal/identifier"
)

type ProbeID string

// Probe describes one dependency check. Check must honor its context, perform
// bounded read-only work and avoid side effects. Error text and unwrapped causes
// never enter readiness reports; use the dependency's explicit diagnostics when
// deeper inspection is authorized.
type Probe struct {
	ID    ProbeID
	Check func(context.Context) error
}

type Config struct {
	MaxProbes     int
	MaxConcurrent int
	CheckTimeout  time.Duration
	ProbeTimeout  time.Duration
}

func DefaultConfig() Config {
	return Config{MaxProbes: 64, MaxConcurrent: 8, CheckTimeout: 3 * time.Second, ProbeTimeout: time.Second}
}

func (c Config) Validate() error {
	if c.MaxProbes < 1 || c.MaxProbes > 256 || c.MaxConcurrent < 1 || c.MaxConcurrent > 64 || c.CheckTimeout <= 0 || c.CheckTimeout > time.Minute || c.ProbeTimeout <= 0 || c.ProbeTimeout > c.CheckTimeout {
		return fault.New(fault.Invalid, "invalid readiness probe resource bounds")
	}
	return nil
}

type State string

const (
	Up             State = "up"
	Down           State = "down"
	TimedOut       State = "timed_out"
	Cancelled      State = "cancelled"
	CallbackFailed State = "callback_failed"
	Closed         State = "closed"
)

type Result struct {
	ID       ProbeID       `json:"id"`
	State    State         `json:"state"`
	Duration time.Duration `json:"duration_ns"`
}

// Report owns its result slice, in declaration order. Every configured probe
// contributes a result even after cancellation. Ready requires every result Up.
type Report struct {
	Ready    bool          `json:"ready"`
	Results  []Result      `json:"results"`
	Duration time.Duration `json:"duration_ns"`
}

// Registry owns callback admission across all concurrent readiness requests.
// Checks within one request run in declaration order under one total timeout.
// A callback ignoring cancellation retains its slot and shutdown ownership until
// it actually returns. This prevents repeated health requests leaking goroutines.
// Construction performs no I/O and starts no goroutines. Do not copy a Registry.
type Registry struct {
	config  Config
	probes  []Probe
	slots   chan struct{}
	mu      sync.Mutex
	closing bool
	active  int
	sealed  chan struct{}
	done    chan struct{}
}

func NewRegistry(config Config, probes ...Probe) (*Registry, error) {
	if err := config.Validate(); err != nil {
		return nil, err
	}
	if len(probes) > config.MaxProbes {
		return nil, fault.New(fault.Invalid, "readiness probe count exceeds its bound")
	}
	seen := make(map[ProbeID]bool, len(probes))
	for _, probe := range probes {
		if len(probe.ID) > 128 || !identifier.Semantic(string(probe.ID)) || probe.Check == nil {
			return nil, fault.New(fault.Invalid, "readiness probe requires an ID and callback")
		}
		if seen[probe.ID] {
			return nil, fault.New(fault.Duplicate, "duplicate readiness probe ID")
		}
		seen[probe.ID] = true
	}
	return &Registry{config: config, probes: append([]Probe(nil), probes...), slots: make(chan struct{}, config.MaxConcurrent), sealed: make(chan struct{}), done: make(chan struct{})}, nil
}

func (r *Registry) Describe() []ProbeID {
	result := make([]ProbeID, len(r.probes))
	for i, probe := range r.probes {
		result[i] = probe.ID
	}
	return result
}

// Check bounds the complete readiness request and each callback. Cancellation
// returns the completed/remaining result states together with the context error.
// A dependency failure returns a non-ready report without exporting its error.
func (r *Registry) Check(ctx context.Context) (Report, error) {
	if r == nil || r.done == nil || ctx == nil {
		return Report{}, fault.New(fault.Invalid, "readiness requires a registry and context")
	}
	started := time.Now()
	ctx, cancel := context.WithTimeout(ctx, r.config.CheckTimeout)
	defer cancel()
	result := Report{Ready: true, Results: make([]Result, 0, len(r.probes))}
	for _, probe := range r.probes {
		item := r.check(ctx, probe)
		result.Results = append(result.Results, item)
		result.Ready = result.Ready && item.State == Up
	}
	select {
	case <-r.sealed:
		result.Ready = false
	default:
	}
	result.Duration = time.Since(started)
	if err := ctx.Err(); err != nil {
		result.Ready = false
		return result, err
	}
	return result, nil
}

func contextState(ctx context.Context) State {
	if ctx.Err() == context.DeadlineExceeded {
		return TimedOut
	}
	return Cancelled
}

func (r *Registry) acquire(ctx context.Context) State {
	select {
	case r.slots <- struct{}{}:
	case <-r.sealed:
		return Closed
	case <-ctx.Done():
		return contextState(ctx)
	}
	r.mu.Lock()
	defer r.mu.Unlock()
	if r.closing {
		<-r.slots
		return Closed
	}
	if ctx.Err() != nil {
		<-r.slots
		return contextState(ctx)
	}
	r.active++
	return Up
}

func (r *Registry) release() {
	r.mu.Lock()
	defer r.mu.Unlock()
	r.active--
	<-r.slots
	if r.closing && r.active == 0 {
		close(r.done)
	}
}

func (r *Registry) check(parent context.Context, probe Probe) Result {
	started := time.Now()
	result := Result{ID: probe.ID}
	ctx, cancel := context.WithTimeout(parent, r.config.ProbeTimeout)
	defer cancel()
	if result.State = r.acquire(ctx); result.State != Up {
		result.Duration = time.Since(started)
		return result
	}
	completed := make(chan State, 1)
	go func() {
		defer r.release()
		state := Down
		err := callback.Isolated("readiness probe", func() error {
			if probe.Check(ctx) == nil {
				state = Up
			}
			return nil
		})
		if err != nil {
			state = CallbackFailed
		}
		if ctx.Err() != nil {
			state = contextState(ctx)
		}
		completed <- state
	}()
	select {
	case result.State = <-completed:
	case <-ctx.Done():
		result.State = contextState(ctx)
	}
	result.Duration = time.Since(started)
	return result
}

// Close seals admission immediately. Its deadline bounds only the caller's
// wait; Done closes when every accepted callback has actually exited.
func (r *Registry) Close(ctx context.Context) error {
	if r == nil || r.done == nil || ctx == nil {
		return fault.New(fault.Invalid, "readiness close requires a registry and context")
	}
	r.mu.Lock()
	if !r.closing {
		r.closing = true
		close(r.sealed)
		if r.active == 0 {
			close(r.done)
		}
	}
	r.mu.Unlock()
	select {
	case <-r.done:
		return nil
	default:
	}
	select {
	case <-r.done:
		return nil
	case <-ctx.Done():
		return ctx.Err()
	}
}

func (r *Registry) Done() <-chan struct{} { return r.done }
