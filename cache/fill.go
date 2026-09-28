package cache

import (
	"context"

	"github.com/weiloon1234/Foundry-Go/fault"
)

// Results are immutable after done closes. Store.mu owns registry and waiter counts.
type fill struct {
	done    chan struct{}
	waiters int
	data    []byte
	err     error
}
type fillContextKey struct{}
type fillContext struct {
	key         EntryKey
	distributed bool
	flight      *fill
	parent      *fillContext
}

func currentFill(ctx context.Context) *fillContext {
	chain, _ := ctx.Value(fillContextKey{}).(*fillContext)
	return chain
}
func (s *Store) joinFill(ctx context.Context, key EntryKey) (*fill, bool, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	if err := ctx.Err(); err != nil {
		return nil, false, err
	}
	if s.coordination != nil {
		for ancestor := currentFill(ctx); ancestor != nil; ancestor = ancestor.parent {
			if ancestor.distributed && ancestor.key == key {
				return nil, false, fault.New(fault.Cycle, "recursive distributed cache fill")
			}
		}
	}
	if existing := s.fills[key]; existing != nil {
		for ancestor := currentFill(ctx); ancestor != nil; ancestor = ancestor.parent {
			if ancestor.flight == existing {
				return nil, false, fault.New(fault.Cycle, "recursive cache fill")
			}
		}
		if existing.waiters >= s.config.MaxFillWaiters {
			return nil, false, fault.New(fault.Conflict, "cache fill waiter limit exceeded")
		}
		existing.waiters++
		return existing, false, nil
	}
	if len(s.fills) >= s.config.MaxFills {
		return nil, false, fault.New(fault.Conflict, "cache fill limit exceeded")
	}
	flight := &fill{done: make(chan struct{})}
	s.fills[key] = flight
	return flight, true, nil
}
func (s *Store) waitFill(ctx context.Context, flight *fill) ([]byte, error) {
	defer func() {
		s.mu.Lock()
		flight.waiters--
		s.mu.Unlock()
	}()
	select {
	case <-ctx.Done():
		return nil, ctx.Err()
	case <-flight.done:
		if err := ctx.Err(); err != nil {
			return nil, err
		}
		return flight.data, flight.err
	}
}
func (s *Store) finishFill(key EntryKey, flight *fill, data []byte, err error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	if err == nil {
		flight.data = data
	}
	flight.err = err
	delete(s.fills, key)
	close(flight.done)
}
