package database

import (
	"context"
	"errors"
	"sync"

	"github.com/weiloon1234/Foundry-Go/database/lifecycle"
	"github.com/weiloon1234/Foundry-Go/internal/contextlink"
)

// operationScope owns one sequential connection/transaction capability. Both
// transaction and session callbacks expire their scope before releasing resources.
type operationScope struct {
	ctx           context.Context
	cancel        context.CancelFunc
	mu            sync.Mutex
	closed        bool
	inUse         bool
	idle          chan struct{}
	rows          *Rows
	observerTasks int
	observersIdle chan struct{}
}

func (s *operationScope) enter() (func() error, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	if s.closed {
		return nil, failure("database scope", Closed)
	}
	if s.inUse {
		return nil, failure("database scope", Busy)
	}
	s.inUse = true
	s.idle = make(chan struct{})
	var once sync.Once
	return func() error {
		once.Do(func() { s.mu.Lock(); defer s.mu.Unlock(); s.inUse = false; s.rows = nil; close(s.idle) })
		return nil
	}, nil
}

func (s *operationScope) track(rows *Rows) error {
	s.mu.Lock()
	if s.closed {
		s.mu.Unlock()
		return errors.Join(failure("database scope", Closed), rows.Close())
	}
	s.rows = rows
	s.mu.Unlock()
	return nil
}

func (s *operationScope) finish() error {
	s.mu.Lock()
	s.closed = true
	busy, idle, rows := s.inUse, s.idle, s.rows
	observers, observersIdle := s.observerTasks, s.observersIdle
	s.mu.Unlock()
	if !busy && observers == 0 {
		return nil
	}
	// Returning with work in flight is misuse. Cancel it and retain ownership
	// until it exits; never commit or reuse its connection underneath that work.
	s.cancel()
	var err error
	if busy {
		if rows != nil {
			err = errors.Join(failure("unclosed database rows", Busy), rows.Close())
		} else {
			<-idle
			err = failure("unfinished database operation", Busy)
		}
	}
	if observers != 0 {
		<-observersIdle
		err = errors.Join(err, failure("unfinished model observer scope", Busy))
	}
	return err
}

func (s *operationScope) context(ctx context.Context) (context.Context, context.CancelFunc) {
	return operationContext(ctx, s.ctx)
}

// Values follow ctx; each owning context can cancel the operation. Stream and
// observer scopes use this same linkage with independent cancellation handles.
func operationContext(ctx context.Context, parents ...context.Context) (context.Context, context.CancelFunc) {
	return contextlink.Link(ctx, parents...)
}

func (s *operationScope) exec(ctx context.Context, executor sqlExecutor, classify classifier, instrument statementProbe, statement string, arguments []any) (Result, error) {
	release, err := s.enter()
	if err != nil {
		return Result{}, err
	}
	defer release()
	operation, cancel := s.context(ctx)
	defer cancel()
	return execute(operation, executor, classify, instrument, statement, arguments)
}

func (s *operationScope) query(ctx context.Context, executor sqlExecutor, classify classifier, instrument statementProbe, observers lifecycle.Observers, statement string, arguments []any) (*Rows, error) {
	release, err := s.enter()
	if err != nil {
		return nil, err
	}
	operation, cancel := s.context(ctx)
	rows, err := query(operation, executor, classify, instrument, observers, observerOwner{scope: s, ctx: ctx}, func() error { cancel(); return release() }, statement, arguments)
	if err != nil {
		return nil, err
	}
	if err := s.track(rows); err != nil {
		return nil, err
	}
	return rows, nil
}
