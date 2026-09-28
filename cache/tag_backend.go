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
