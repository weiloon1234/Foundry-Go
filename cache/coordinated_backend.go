package cache

import (
	"context"

	"github.com/weiloon1234/Foundry-Go/lease"
)

// CoordinatedBackend atomically checks a live lease and publishes a value using
// the same authority as the lease Backend. Validate the proof before I/O, then
// compare its complete address/owner and finite expiry in the write operation.
// Missing/replaced/expired ownership returns lease.ErrLost without changing data.
// Payload validation and ordinary Put semantics still apply. Never retry a write.
type CoordinatedBackend interface {
	Backend
	PutLeased(context.Context, EntryKey, []byte, TTL, lease.Proof) error
}

// CoordinatedTaggedBackend checks both ownership and the tag snapshot atomically
// with publication. A stale tag returns Conflict and cannot replace newer data.
type CoordinatedTaggedBackend interface {
	TaggedBackend
	PutTaggedLeased(context.Context, TaggedKey, []byte, TTL, lease.Proof) error
}
