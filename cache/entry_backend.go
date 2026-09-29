package cache

import "context"

// EntryBackend inspects existence and updates expiry without materializing payloads.
// Methods validate the adapter's stored type/envelope/size, but do not run a value
// codec. Expire returns true for a current entry even if its expiry is unchanged.
// Missing entries are never created. Context, lifecycle and uncertain-write rules
// are the same as Backend. No read/write fallback or retry is permitted.
type EntryBackend interface {
	Exists(context.Context, EntryKey) (bool, error)
	Expire(context.Context, EntryKey, TTL) (bool, error)
}

// TaggedEntryBackend checks a current tag snapshot atomically with inspection or
// expiry mutation. Stale callers return Conflict; obsolete stored entries are
// logically absent and may be reclaimed, without decoding their old payloads.
type TaggedEntryBackend interface {
	ExistsTagged(context.Context, TaggedKey) (bool, error)
	ExpireTagged(context.Context, TaggedKey, TTL) (bool, error)
}

// BatchBackend removes a bounded canonical batch in one atomic operation. All
// validation precedes deletion. Count distinct live entries; preserve all live
// entries if a later key fails validation. Errors return zero, but a remote error
// may hide an applied whole batch. Adapters validate with ValidateBatchKeys.
type BatchBackend interface {
	ForgetMany(context.Context, []EntryKey) (uint64, error)
}

// TaggedBatchBackend additionally requires every entry to share one tag snapshot.
// Missing/changed metadata returns Conflict before deletion. Old stored generations
// are logically absent and do not increase the live removal count. Adapters share
// ValidateTaggedBatch; metadata and unrelated keys must remain untouched.
type TaggedBatchBackend interface {
	ForgetManyTagged(context.Context, []TaggedKey) (uint64, error)
}

// BatchValue is one adapter batch-read result. Found distinguishes a miss from a
// present empty payload; Data is owned by the caller.
type BatchValue struct {
	Data  []byte
	Found bool
}

// BatchReadBackend reads a bounded canonical batch (see ValidateBatchKeys) in one
// operation, returning results in input order. Missing, expired or unusable
// entries are misses. Size bounds follow Get; nothing is mutated except removal
// of unusable storage.
type BatchReadBackend interface {
	GetMany(context.Context, []EntryKey) ([]BatchValue, error)
}

// TaggedBatchReadBackend reads a canonical batch sharing one tag snapshot (see
// ValidateTaggedBatch) atomically with its validation. A changed or missing
// snapshot returns Conflict; obsolete payloads are misses and may be reclaimed.
type TaggedBatchReadBackend interface {
	GetManyTagged(context.Context, []TaggedKey) ([]BatchValue, error)
}
