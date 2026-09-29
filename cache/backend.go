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

// FlushBackend physically removes every entry of one cache namespace. Adapters
// without tag metadata (file and PostgreSQL) implement it so Store.Invalidate
// works for every store type. It must not remove other namespaces' entries and
// must serialize with the adapter's single-entry operations, but it is not a
// fence: a Remember fill that started earlier can publish afterwards. It returns
// the number of removed entries; a remote error may hide a partial removal.
type FlushBackend interface {
	FlushNamespace(context.Context, Namespace) (uint64, error)
}
