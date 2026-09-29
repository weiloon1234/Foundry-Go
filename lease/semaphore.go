package lease

import (
	"context"
	"math/rand/v2"
	"strconv"
	"time"

	"github.com/weiloon1234/Foundry-Go/fault"
	"github.com/weiloon1234/Foundry-Go/keyspace"
)

// MaxSemaphoreSlots bounds the leases one semaphore resource can hold.
const MaxSemaphoreSlots = 1024

// semaphoreProbes bounds the slots a waiting acquisition tries per poll after
// its first full pass, so a busy semaphore costs a few commands per interval.
const semaphoreProbes = 8

// semaphoreSlot addresses one numbered lease of a semaphore resource.
type semaphoreSlot[K any] struct {
	key  K
	slot int
}

// SemaphoreDeclaration is a typed distributed concurrency limit: at most Slots
// holders of the same resource key at once across every process sharing the
// lease authority. Each holder owns one ordinary lease, so expiry, renewal,
// loss and cleanup follow the lease contract. Reuse one declared value.
type SemaphoreDeclaration[K any] struct {
	leases Declaration[semaphoreSlot[K]]
	slots  int
}

// DefineSemaphore declares a family limiting each resource key to slots holders.
func DefineSemaphore[K any](name Name, codec keyspace.Codec[K], slots int) SemaphoreDeclaration[K] {
	encode := keyspace.NewCodec(func(s semaphoreSlot[K]) (string, error) {
		logical, err := codec.Encode(s.key)
		if err != nil {
			return "", err
		}
		// The numeric suffix is always last, so distinct (key, slot) pairs
		// never produce the same logical key within this family.
		return logical + "#" + strconv.Itoa(s.slot), nil
	})
	if codec.Validate() != nil {
		encode = keyspace.Codec[semaphoreSlot[K]]{}
	}
	return SemaphoreDeclaration[K]{leases: Define(name, encode), slots: slots}
}
func (d SemaphoreDeclaration[K]) Name() Name { return d.leases.Name() }
func (d SemaphoreDeclaration[K]) Validate() error {
	if d.slots < 1 || d.slots > MaxSemaphoreSlots {
		return fault.New(fault.Invalid, "semaphore slots must be between 1 and 1024")
	}
	return d.leases.Validate()
}

// Bind shares the manager's declaration ownership, limits and lifecycle.
func (d SemaphoreDeclaration[K]) Bind(m *Manager) (Semaphore[K], error) {
	if err := d.Validate(); err != nil {
		return Semaphore[K]{}, err
	}
	leases, err := d.leases.Bind(m)
	if err != nil {
		return Semaphore[K]{}, err
	}
	return Semaphore[K]{leases: leases, slots: d.slots}, nil
}

// Semaphore retains its declaration's concrete resource key type.
type Semaphore[K any] struct {
	leases Leases[semaphoreSlot[K]]
	slots  int
}

// Slots is the declared number of concurrent holders per resource key.
func (s Semaphore[K]) Slots() int { return s.slots }

// Acquire takes any free slot, waiting up to wait while all slots are held.
// The first attempt tries every slot once in random order (a zero wait stops
// there); each later poll tries at most 8 random slots. Like Leases.Acquire, the guard does not renew
// automatically; contention returns (nil, false, nil) and backend errors stop
// immediately without being treated as contention.
func (s Semaphore[K]) Acquire(ctx context.Context, key K, ttl, wait time.Duration) (*Guard, bool, error) {
	var guard *Guard
	ok, err := s.poll(ctx, wait, func(slot int) (bool, error) {
		g, ok, err := s.leases.TryAcquire(ctx, semaphoreSlot[K]{key, slot}, ttl)
		guard = g
		return ok, err
	})
	if err != nil || !ok {
		return nil, false, err
	}
	return guard, true, nil
}

// With runs fn while holding a slot, renewing it like Leases.With. It reports
// whether fn ran. Contention within wait is (false, nil).
func (s Semaphore[K]) With(ctx context.Context, key K, ttl, wait time.Duration, fn func(context.Context) error) (bool, error) {
	if fn == nil {
		return false, fault.New(fault.Invalid, "semaphore callback is required")
	}
	return s.poll(ctx, wait, func(slot int) (bool, error) {
		return s.leases.With(ctx, semaphoreSlot[K]{key, slot}, ttl, 0, fn)
	})
}

// poll tries every slot once in a random order, then after each jittered poll
// interval a bounded random subset, until one attempt succeeds, fails, or the
// wait expires.
func (s Semaphore[K]) poll(ctx context.Context, wait time.Duration, attempt func(int) (bool, error)) (bool, error) {
	if s.leases.definition == nil {
		return false, fault.New(fault.Invalid, "semaphore handle is not initialized")
	}
	if ctx == nil {
		return false, fault.New(fault.Invalid, "semaphore requires a context")
	}
	if wait < 0 || wait > s.leases.manager.config.MaxWait {
		return false, fault.New(fault.Invalid, "lease wait exceeds its bound")
	}
	deadline := time.Now().Add(wait)
	order := rand.Perm(s.slots)
	probes := len(order)
	for {
		for _, slot := range order[:probes] {
			ok, err := attempt(slot)
			if err != nil || ok {
				return ok, err
			}
		}
		// Later rounds try a fresh random subset (a partial Fisher-Yates shuffle).
		probes = min(len(order), semaphoreProbes)
		for i := range probes {
			j := i + rand.IntN(len(order)-i)
			order[i], order[j] = order[j], order[i]
		}
		remaining := time.Until(deadline)
		if wait == 0 {
			return false, nil
		}
		if remaining <= 0 {
			return false, context.DeadlineExceeded
		}
		poll := s.leases.manager.config.PollInterval
		delay := poll/2 + time.Duration(rand.Int64N(int64(poll-poll/2)))
		timer := time.NewTimer(min(delay, remaining))
		select {
		case <-ctx.Done():
			timer.Stop()
			return false, ctx.Err()
		case <-timer.C:
		}
	}
}
