package cache

import "context"

// TaggedBackend adds atomic tag metadata and conditional tagged entry operations
// to a Backend. ResolveTags returns versions in input order, creating fresh random
// versions for missing metadata. Inputs are canonical and bounded; adapters must
// still validate their public boundary. InvalidateTags replaces all selected
// versions atomically. Missing metadata never reuses an earlier/default version.
//
// Tagged operations must reject a changed/missing snapshot with fault.Conflict
// before altering data. Get/Add treat an older stored fingerprint as absent; a
// conditional cleanup cannot delete a newer replacement. Storage addresses stay
// stable across versions. Size, ownership and corruption failures preserve live
// entries. All methods must observe context and share Backend lifecycle/limits.
type TaggedBackend interface {
	ResolveTags(context.Context, []EntryKey) ([]TagVersion, error)
	InvalidateTags(context.Context, []EntryKey) error
	GetTagged(context.Context, TaggedKey) ([]byte, bool, error)
	PutTagged(context.Context, TaggedKey, []byte, TTL) error
	AddTagged(context.Context, TaggedKey, []byte, TTL) (bool, error)
	ForgetTagged(context.Context, TaggedKey) (bool, error)
}

// TaggedCounterBackend is the optional exact counter capability for tagged views.
// It combines snapshot validation and Increment semantics in one atomic operation.
type TaggedCounterBackend interface {
	IncrementTagged(context.Context, TaggedKey, int64, TTL) (int64, error)
}

// SnapshotReadBackend optionally combines ResolveTags with a tagged read in one
// atomic operation, halving round trips for Get, Exists and Remember lookups.
// tags are canonical metadata addresses (see ValidateTagKeys); missing metadata
// receives fresh random versions exactly as in ResolveTags.
// The returned key must be NewTaggedKey(base, stamps) for the versions read.
// found reports a current, readable payload for that key; payload=false skips
// transferring the value. An obsolete or over-bound stored payload is a miss.
type SnapshotReadBackend interface {
	ReadSnapshot(ctx context.Context, base EntryKey, tags []EntryKey, payload bool) (TaggedKey, []byte, bool, error)
}

// SnapshotWriteBackend optionally resolves tag metadata (tags are canonical
// metadata addresses; missing metadata receives fresh versions exactly as in
// ResolveTags) and applies one tagged mutation under the resolved snapshot in
// the same atomic operation. Direct Put, Add, Forget, Increment and Expire then
// cost one round trip and never cross an invalidation; Remember publications
// keep the snapshot they read. Results and failures follow the matching
// TaggedBackend, TaggedCounterBackend and TaggedEntryBackend methods.
type SnapshotWriteBackend interface {
	PutSnapshot(ctx context.Context, base EntryKey, tags []EntryKey, data []byte, ttl TTL) error
	AddSnapshot(ctx context.Context, base EntryKey, tags []EntryKey, data []byte, ttl TTL) (bool, error)
	ForgetSnapshot(ctx context.Context, base EntryKey, tags []EntryKey) (bool, error)
	IncrementSnapshot(ctx context.Context, base EntryKey, tags []EntryKey, delta int64, ttl TTL) (int64, error)
	ExpireSnapshot(ctx context.Context, base EntryKey, tags []EntryKey, ttl TTL) (bool, error)
}
