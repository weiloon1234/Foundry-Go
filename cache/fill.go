package cache

import (
	"context"

	"github.com/weiloon1234/Foundry-Go/fault"
)

// fillAttempts bounds how often a follower re-elects after an owner's fill
// failed only because that owner's own request context ended.
const fillAttempts = 3

// Results are immutable after done closes. Store.mu owns registry and waiter counts.
type fill struct {
	done    chan struct{}
	waiters int
	data    []byte
	err     error
	outcome fillOutcome
}

// fillOutcome qualifies a fill result for its callers.
type fillOutcome struct {
	// retry marks a failure caused only by the electing caller's own context
	// (a loader that ignored its supplied context); followers elect again.
	retry bool
	// unpublished marks a loaded value that could not be stored.
	unpublished bool
}
type fillContextKey struct{}

// fillContext is one link of a loader's fill chain (coalesced or direct), used
// to detect recursive loads of the same key.
type fillContext struct {
	store       *Store
	key         EntryKey
	distributed bool
	parent      *fillContext
}

// fillRole is a caller's position in one fill registration.
type fillRole uint8

const (
	fillFollower fillRole = iota
	fillOwner
	// fillUncoalesced means MaxFills is exhausted: the caller loads directly
	// rather than failing, without registering a coalesced fill.
	fillUncoalesced
)

func currentFill(ctx context.Context) *fillContext {
	chain, _ := ctx.Value(fillContextKey{}).(*fillContext)
	return chain
}

// joinFill registers the caller for key. An owner or uncoalesced caller counts
// as a running fill until finishFill or endDirect; a closed Store admits none.
func (s *Store) joinFill(ctx context.Context, key EntryKey) (*fill, fillRole, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	if err := s.active(); err != nil {
		return nil, fillFollower, err
	}
	if err := ctx.Err(); err != nil {
		return nil, fillFollower, err
	}
	for ancestor := currentFill(ctx); ancestor != nil; ancestor = ancestor.parent {
		// A loader reloading its own key (coalesced or direct) is a cycle, and
		// so is any distributed fill of the key while its lease is held.
		if ancestor.key == key && (ancestor.store == s || s.coordination != nil && ancestor.distributed) {
			return nil, fillFollower, fault.New(fault.Cycle, "recursive cache fill")
		}
	}
	if existing := s.fills[key]; existing != nil {
		if existing.waiters >= s.config.MaxFillWaiters {
			return nil, fillFollower, fault.New(fault.Overloaded, "cache fill waiter capacity is exhausted")
		}
		existing.waiters++
		return existing, fillFollower, nil
	}
	s.running++
	if len(s.fills) >= s.config.MaxFills {
		return nil, fillUncoalesced, nil
	}
	flight := &fill{done: make(chan struct{})}
	s.fills[key] = flight
	return flight, fillOwner, nil
}

// waitFill ends only this caller's wait; the fill keeps running for others.
func (s *Store) waitFill(ctx context.Context, flight *fill, counted bool) ([]byte, fillOutcome, error) {
	if counted {
		defer func() {
			s.mu.Lock()
			flight.waiters--
			s.mu.Unlock()
		}()
	}
	select {
	case <-ctx.Done():
		return nil, fillOutcome{}, ctx.Err()
	case <-flight.done:
		if err := ctx.Err(); err != nil {
			return nil, fillOutcome{}, err
		}
		return flight.data, flight.outcome, flight.err
	}
}

// leaveFill releases a follower registration without waiting for the result.
func (s *Store) leaveFill(flight *fill) {
	s.mu.Lock()
	defer s.mu.Unlock()
	flight.waiters--
}

// endDirect ends an uncoalesced load's running registration.
func (s *Store) endDirect() {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.running--
	s.finishLocked()
}
func (s *Store) finishFill(key EntryKey, flight *fill, data []byte, err error, outcome fillOutcome) {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.running--
	defer s.finishLocked()
	if err == nil {
		flight.data = data
	}
	flight.err = err
	outcome.retry = err != nil && outcome.retry
	flight.outcome = outcome
	delete(s.fills, key)
	close(flight.done)
}
