package raw

import (
	"context"
	"sync"

	"github.com/weiloon1234/Foundry-Go/fault"
)

type Mode uint8

const (
	Pipelined Mode = iota + 1
	Transaction
)

func (m Mode) Validate() error {
	if m != Pipelined && m != Transaction {
		return fault.New(fault.Invalid, "invalid Redis pipeline mode")
	}
	return nil
}

type queued struct {
	request Request
	prepare func(context.Context, Reply) (func(), error)
}

// Pipeline is built once and executed once. Queue operations and Run are serialized;
// whichever obtains the lock first determines inclusion. Execution consumes the
// pipeline even on failure. Construct another pipeline for a deliberate new attempt.
type Pipeline struct {
	mu      sync.Mutex
	mode    Mode
	entries []queued
	bytes   int
	started bool
	done    chan struct{}
	err     error
}

func NewPipeline(mode Mode) (*Pipeline, error) {
	if err := mode.Validate(); err != nil {
		return nil, err
	}
	return &Pipeline{mode: mode, done: make(chan struct{})}, nil
}

type resultState[R any] struct{ value R }

// Result is a typed pipeline receipt. Value is available only after the entire
// execution and all decoders succeed. Repeated Value calls share the application's
// decoded value; callers own synchronization of any maps/slices it contains.
type Result[R any] struct {
	pipeline *Pipeline
	state    *resultState[R]
}

func (r Result[R]) Value() (R, error) {
	if r.pipeline == nil || r.state == nil {
		return *new(R), fault.New(fault.Invalid, "invalid Redis pipeline result")
	}
	select {
	case <-r.pipeline.done:
	default:
		return *new(R), fault.New(fault.Conflict, "Redis pipeline result is not ready")
	}
	if r.pipeline.err != nil {
		return *new(R), r.pipeline.err
	}
	return r.state.value, nil
}
func Queue[R any](p *Pipeline, c Command[R]) (Result[R], error) {
	state := &resultState[R]{}
	if c.decoder.decode == nil {
		return Result[R]{}, replyError()
	}
	err := p.enqueue(queued{c.request, func(ctx context.Context, reply Reply) (func(), error) {
		v, err := c.decoder.decode(ctx, reply)
		if err != nil {
			return nil, err
		}
		return func() { state.value = v }, nil
	}})
	if err != nil {
		return Result[R]{}, err
	}
	return Result[R]{p, state}, nil
}

// Ignore still validates the typed decoder and every server/protocol error; it
// discards only the successful decoded value, never a failed command.
func Ignore[R any](p *Pipeline, c Command[R]) error {
	if c.decoder.decode == nil {
		return replyError()
	}
	return p.enqueue(queued{c.request, func(ctx context.Context, reply Reply) (func(), error) {
		_, err := c.decoder.decode(ctx, reply)
		return func() {}, err
	}})
}
func (p *Pipeline) enqueue(q queued) error {
	if p == nil || p.done == nil {
		return fault.New(fault.Invalid, "Redis pipeline is uninitialized")
	}
	p.mu.Lock()
	defer p.mu.Unlock()
	if p.started {
		return fault.New(fault.Conflict, "Redis pipeline has already started")
	}
	if q.request.invalid || len(p.entries) >= MaxCommands || q.request.Bytes() > MaxRequestBytes-p.bytes {
		return fault.New(fault.Invalid, "Redis pipeline exceeds its construction bound")
	}
	p.entries = append(p.entries, q)
	p.bytes += q.request.Bytes()
	return nil
}
func (p *Pipeline) Run(ctx context.Context, s *Store) error {
	if p == nil || p.done == nil {
		return fault.New(fault.Invalid, "Redis pipeline is uninitialized")
	}
	p.mu.Lock()
	if p.started {
		p.mu.Unlock()
		return fault.New(fault.Conflict, "Redis pipeline has already started")
	}
	p.started = true
	entries := p.entries
	p.entries = nil
	p.mu.Unlock()
	p.err = s.execute(ctx, func(ctx context.Context) error {
		if len(entries) > s.config.Limits.Commands || p.bytes > s.config.Limits.RequestBytes {
			return fault.New(fault.Invalid, "Redis pipeline exceeds its configured bound")
		}
		requests := make([]Request, len(entries))
		for i, e := range entries {
			if err := e.request.Validate(s.config.Namespace, s.config.Limits); err != nil {
				return err
			}
			requests[i] = e.request
		}
		if len(entries) == 0 {
			return nil
		}
		replies, err := s.backend.ExecuteRawBatch(ctx, requests, p.mode, s.config.Limits)
		if err != nil {
			return err
		}
		if len(replies) != len(entries) {
			return fault.New(fault.Internal, "Redis pipeline reply count does not match its commands")
		}
		if err := validateReplies(ctx, replies, s.config.Limits.Reply, true); err != nil {
			return err
		}
		publish := make([]func(), len(entries))
		for i, e := range entries {
			if err := ctx.Err(); err != nil {
				return err
			}
			commit, err := e.prepare(ctx, replies[i])
			if err != nil {
				return err
			}
			publish[i] = commit
		}
		if err := ctx.Err(); err != nil {
			return err
		}
		for _, commit := range publish {
			commit()
		}
		return nil
	})
	close(p.done)
	return p.err
}

// Len reports queued commands. Run claims and clears that queue before I/O.
func (p *Pipeline) Len() int {
	if p == nil {
		return 0
	}
	p.mu.Lock()
	defer p.mu.Unlock()
	return len(p.entries)
}
func (p *Pipeline) IsEmpty() bool { return p.Len() == 0 }
func (p *Pipeline) Mode() Mode {
	if p == nil {
		return 0
	}
	return p.mode
}
