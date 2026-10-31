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
	// ResolveTags returns versions in input order, creating fresh versions for missing metadata.
	ResolveTags(context.Context, []EntryKey) ([]TagVersion, error)
	// InvalidateTags atomically replaces all selected tag versions.
	InvalidateTags(context.Context, []EntryKey) error
	// GetTagged returns owned bytes under the current snapshot, treating obsolete stored values as absent.
	GetTagged(context.Context, TaggedKey) ([]byte, bool, error)
	// PutTagged replaces the entry only if the tag snapshot is current.
	PutTagged(context.Context, TaggedKey, []byte, TTL) error
	// AddTagged stores the entry only if the snapshot is current and no current value exists.
	AddTagged(context.Context, TaggedKey, []byte, TTL) (bool, error)
	// ForgetTagged removes the current value only under a current tag snapshot.
	ForgetTagged(context.Context, TaggedKey) (bool, error)
}

// TaggedCounterBackend is the optional exact counter capability for tagged views.
// It combines snapshot validation and Increment semantics in one atomic operation.
type TaggedCounterBackend interface {
	// IncrementTagged atomically increments the counter under a current tag snapshot.
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
	// ReadSnapshot atomically resolves tag versions and checks the entry, returning bytes only when payload is true.
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
	// PutSnapshot resolves tags and replaces the entry atomically.
	PutSnapshot(ctx context.Context, base EntryKey, tags []EntryKey, data []byte, ttl TTL) error
	// AddSnapshot resolves tags and inserts an absent current entry atomically.
	AddSnapshot(ctx context.Context, base EntryKey, tags []EntryKey, data []byte, ttl TTL) (bool, error)
	// ForgetSnapshot resolves tags and removes the current entry atomically.
	ForgetSnapshot(ctx context.Context, base EntryKey, tags []EntryKey) (bool, error)
	// IncrementSnapshot resolves tags and increments the current counter atomically.
	IncrementSnapshot(ctx context.Context, base EntryKey, tags []EntryKey, delta int64, ttl TTL) (int64, error)
	// ExpireSnapshot resolves tags and changes a current entry's expiry atomically.
	ExpireSnapshot(ctx context.Context, base EntryKey, tags []EntryKey, ttl TTL) (bool, error)
}
