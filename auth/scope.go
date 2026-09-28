package auth

import (
	"context"

	"github.com/weiloon1234/Foundry-Go/fault"
	"github.com/weiloon1234/Foundry-Go/internal/callback"
)

type operationKey struct{}
type operationFrame struct {
	scope  *Scope
	id     *declarationID
	parent *operationFrame
}

func recursive(ctx context.Context, s *Scope, id *declarationID) bool {
	frame, _ := ctx.Value(operationKey{}).(*operationFrame)
	for ; frame != nil; frame = frame.parent {
		if frame.scope == s && frame.id == id {
			return true
		}
	}
	return false
}
func (s *Scope) check(ctx context.Context) error {
	if s == nil || s.registry == nil || s.ctx == nil || ctx == nil {
		return fault.New(fault.Invalid, "authentication requires a scope and context")
	}
	if err := ctx.Err(); err != nil {
		return err
	}
	s.mu.Lock()
	closed := s.closed
	s.mu.Unlock()
	if closed {
		return fault.New(fault.Closed, "authentication scope is closed")
	}
	return s.ctx.Err()
}

// execute is called only while the scope's active ownership is held.
func (s *Scope) execute(ctx context.Context, id *declarationID, fn func(context.Context) error) error {
	parent, _ := ctx.Value(operationKey{}).(*operationFrame)
	ctx = context.WithValue(ctx, operationKey{}, &operationFrame{scope: s, id: id, parent: parent})
	op, cancel := context.WithTimeout(ctx, s.registry.config.Timeout)
	stop := context.AfterFunc(s.ctx, cancel)
	defer stop()
	defer cancel()
	if err := s.ctx.Err(); err != nil {
		return err
	}
	if err := op.Err(); err != nil {
		return err
	}
	select {
	case s.registry.slots <- struct{}{}:
	default:
		return fault.New(fault.Conflict, "authentication operation capacity reached")
	}
	defer func() { <-s.registry.slots }()
	err := callback.Isolated("authentication callback", func() error {
		if err := op.Err(); err != nil {
			return err
		}
		return fn(op)
	})
	if canceled := s.ctx.Err(); canceled != nil {
		return canceled
	}
	if canceled := op.Err(); canceled != nil {
		return canceled
	}
	return operationFailure(err)
}

func (s *Scope) resolve(ctx context.Context, id *declarationID, fn func(context.Context, Credentials) (any, error)) (any, error) {
	if err := s.check(ctx); err != nil {
		return nil, err
	}
	if !s.registry.guards[id] {
		return nil, fault.New(fault.Missing, "authentication guard is not registered in this scope")
	}
	if recursive(ctx, s, id) {
		return nil, fault.New(fault.Cycle, "recursive authentication guard resolution")
	}
	s.mu.Lock()
	if s.closed {
		s.mu.Unlock()
		return nil, fault.New(fault.Closed, "authentication scope is closed")
	}
	entry, exists := s.guards[id]
	if !exists {
		entry = &resolutionEntry{done: make(chan struct{})}
		s.guards[id] = entry
		s.active.Add(1)
	}
	credentials := s.credentials
	s.mu.Unlock()
	if !exists {
		defer s.active.Done()
		var result any
		err := s.execute(ctx, id, func(op context.Context) error { var err error; result, err = fn(op, credentials); return err })
		if err == nil {
			entry.value = result
		}
		entry.err = err
		close(entry.done)
	}
	select {
	case <-ctx.Done():
		return nil, ctx.Err()
	case <-s.ctx.Done():
		return nil, s.ctx.Err()
	case <-entry.done:
		if err := s.check(ctx); err != nil {
			return nil, err
		}
		return entry.value, entry.err
	}
}

func (s *Scope) evaluate(ctx context.Context, id *declarationID, fn func(context.Context) error) error {
	if err := s.check(ctx); err != nil {
		return err
	}
	if !s.registry.policies[id] {
		return fault.New(fault.Missing, "authentication policy is not registered in this scope")
	}
	if recursive(ctx, s, id) {
		return fault.New(fault.Cycle, "recursive authentication policy evaluation")
	}
	s.mu.Lock()
	if s.closed {
		s.mu.Unlock()
		return fault.New(fault.Closed, "authentication scope is closed")
	}
	s.active.Add(1)
	s.mu.Unlock()
	defer s.active.Done()
	return s.execute(ctx, id, fn)
}
