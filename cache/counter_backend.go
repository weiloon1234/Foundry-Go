package cache

import "context"

// CounterBackend is an optional capability of the Backend used by a Store.
// Increment must be atomic with Get/Put/Add/Forget on the same EntryKey. Its
// representation is canonical signed decimal int64, shared with Counter values.
// A missing or expired entry starts at zero; initialTTL applies only to creation.
// Existing expiry is retained. Corrupt data, overflow and capacity rejection must
// leave an existing entry unchanged. Invalid initialTTL is always an error.
// Failed remote writes may still have been applied; do not retry implicitly.
type CounterBackend interface {
	Increment(ctx context.Context, key EntryKey, delta int64, initialTTL TTL) (int64, error)
}
