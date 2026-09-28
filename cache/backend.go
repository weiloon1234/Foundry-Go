package cache

import "context"

// Backend is the serialized cache adapter boundary, not an application value API.
// Implementations must be concurrency-safe, observe cancellation, copy retained
// input bytes without modifying the caller's input and return owned read bytes. Get distinguishes absence from failure;
// a present empty byte slice is a hit. Add checks absence and writes atomically.
// A failed mutating command may have reached a remote backend: never retry it
// implicitly. Adapter lifecycle is owned by the caller that constructs the backend.
// Store borrows this capability and does not close a shared adapter.
type Backend interface {
	Get(context.Context, EntryKey) ([]byte, bool, error)
	Put(context.Context, EntryKey, []byte, TTL) error
	Add(context.Context, EntryKey, []byte, TTL) (bool, error)
	Forget(context.Context, EntryKey) (bool, error)
}
