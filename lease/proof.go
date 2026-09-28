package lease

import (
	"context"
	"time"
)

// Proof is an opaque adapter capability for one live lease owner. An adapter
// must validate it locally AND atomically compare Key/Owner at its authority when
// applying a protected mutation. Validate alone cannot fence a subsequent write.
// Proof cannot outlive its guard and must not be serialized into durable work.
type Proof struct{ guard *Guard }

// Proof exposes this guard's ownership to an adapter, without allowing callers
// to construct an arbitrary key/token pair. Ordinary domain work needs Context.
func (g *Guard) Proof() (Proof, error) {
	if err := g.Err(); err != nil {
		return Proof{}, err
	}
	return Proof{guard: g}, nil
}
func (p Proof) Validate() error { return p.guard.Err() }

// Key is the complete namespaced lease address for atomic authority checks.
func (p Proof) Key() Key {
	if p.guard == nil {
		return Key{}
	}
	return p.guard.key
}

// Owner is the opaque token for adapter wire encoding; it is not a fencing sequence.
func (p Proof) Owner() Owner {
	if p.guard == nil {
		return Owner{}
	}
	return p.guard.owner
}
func (p Proof) String() string   { return "[lease proof]" }
func (p Proof) GoString() string { return p.String() }

// WithProof is With for feature/adaptor integration. The callback receives a live
// proof that its backend must check atomically with any protected write. It shares
// With's heartbeat, cancellation, callback isolation, limits and cleanup semantics.
func (l Leases[K]) WithProof(ctx context.Context, key K, ttl, wait time.Duration, fn func(context.Context, Proof) error) (bool, error) {
	return l.with(ctx, key, ttl, wait, fn)
}
