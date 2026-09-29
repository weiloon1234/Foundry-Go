package database

import (
	"context"
	"errors"
	"sync"

	"github.com/weiloon1234/Foundry-Go/fault"
	"github.com/weiloon1234/Foundry-Go/internal/callback"
)

// observerOwner records the actual query caller and one resource owner. Its
// context precedes the stream context, which is canceled when rows close.
type observerOwner struct {
	db    *DB
	scope *operationScope
	ctx   context.Context
}

// WithObserverScope is the generated read adapter's lifetime boundary. Call it
// before iterating or closing this stream. Each stream accepts one scope. The
// callback must close rows before issuing SQL on their transaction or session;
// this method also closes rows on callback return, error, panic or Goexit.
//
// Closing the stream releases its SQL operation, while this scope retains work
// ownership until fn exits. The callback context keeps ctx's values and observes
// ctx, the actual query caller and any owning transaction/session cancellation.
// It expires when fn returns. Declared model hooks are not dispatched here.
func (r *Rows) WithObserverScope(ctx context.Context, fn func(context.Context) error) (err error) {
	if ctx == nil || fn == nil {
		return fault.New(fault.Invalid, "model observer scope requires a context and callback")
	}
	if err := ctx.Err(); err != nil {
		return err
	}
	work, finish, err := r.beginObserverScope(ctx)
	if err != nil {
		return err
	}
	defer finish()
	defer func() { err = errors.Join(err, r.Close()) }()
	if err := work.Err(); err != nil {
		return err
	}
	err = callback.Isolated("model observer scope", func() error { return fn(work) })
	if err == nil {
		err = work.Err()
	}
	return err
}

func (r *Rows) beginObserverScope(ctx context.Context) (context.Context, func(), error) {
	r.mu.Lock()
	defer r.mu.Unlock()
	if r.closed {
		return nil, nil, failure("model observer scope", Closed)
	}
	if r.observerScoped {
		return nil, nil, failure("model observer scope", Busy)
	}
	work, finish, err := r.observerOwner.retain(ctx)
	if err == nil {
		r.observerScoped = true
	}
	return work, finish, err
}

// Called while the originating Rows mutex prevents its resource lease from
// being released. A pool continuation may therefore survive Close beginning.
func (o observerOwner) retain(ctx context.Context) (context.Context, func(), error) {
	if o.ctx == nil || (o.db == nil) == (o.scope == nil) {
		return nil, nil, fault.New(fault.Invalid, "row stream has no model observer scope owner")
	}
	parents := []context.Context{o.ctx}
	if o.scope != nil {
		parents = append(parents, o.scope.ctx)
	}
	work, cancel := operationContext(ctx, parents...)
	var release func()
	var err error
	if o.db != nil {
		release, err = o.db.retainObserverOwner()
	} else {
		release, err = o.scope.retainObserverOwner()
	}
	if err != nil {
		cancel()
		return nil, nil, err
	}
	return work, sync.OnceFunc(func() { cancel(); release() }), nil
}

func (db *DB) retainObserverOwner() (func(), error) {
	// The still-open originating stream already owns a resource. Do not reject
	// its continuation merely because Close has started draining existing work.
	for {
		current := db.owners.Load()
		if current&ownerReady == 0 || current&ownerCount == 0 || current&ownerCount == ownerCount {
			return nil, failure("model observer scope", Closed)
		}
		if db.owners.CompareAndSwap(current, current+1) {
			return db.release, nil
		}
	}
}

func (s *operationScope) retainObserverOwner() (func(), error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	if s.closed {
		return nil, failure("model observer scope", Closed)
	}
	if s.observerTasks == 0 {
		s.observersIdle = make(chan struct{})
	}
	s.observerTasks++
	return func() {
		s.mu.Lock()
		defer s.mu.Unlock()
		s.observerTasks--
		if s.observerTasks == 0 {
			close(s.observersIdle)
		}
	}, nil
}
