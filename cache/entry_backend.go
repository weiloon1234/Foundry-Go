package cache

import "context"

// EntryBackend inspects existence and updates expiry without materializing payloads.
// Methods validate the adapter's stored type/envelope/size, but do not run a value
// codec. Expire returns true for a current entry even if its expiry is unchanged.
// Missing entries are never created. Context, lifecycle and uncertain-write rules
// are the same as Backend. No read/write fallback or retry is permitted.
type EntryBackend interface {
	// Exists reports whether a usable live entry is present without decoding its value.
	Exists(context.Context, EntryKey) (bool, error)
	// Expire changes a live entry's TTL and reports its presence without creating missing entries.
	Expire(context.Context, EntryKey, TTL) (bool, error)
}

// TaggedEntryBackend checks a current tag snapshot atomically with inspection or
// expiry mutation. Stale callers return Conflict; obsolete stored entries are
// logically absent and may be reclaimed, without decoding their old payloads.
type TaggedEntryBackend interface {
	// ExistsTagged checks entry existence under the supplied current tag snapshot.
	ExistsTagged(context.Context, TaggedKey) (bool, error)
	// ExpireTagged changes expiry only under the supplied current tag snapshot.
	ExpireTagged(context.Context, TaggedKey, TTL) (bool, error)
}

// BatchBackend removes a bounded canonical batch in one atomic operation. All
// validation precedes deletion. Count distinct live entries; preserve all live
// entries if a later key fails validation. Errors return zero, but a remote error
// may hide an applied whole batch. Adapters validate with ValidateBatchKeys.
type BatchBackend interface {
	// ForgetMany atomically removes the canonical batch and counts distinct live entries.
	ForgetMany(context.Context, []EntryKey) (uint64, error)
}

// TaggedBatchBackend additionally requires every entry to share one tag snapshot.
// Missing/changed metadata returns Conflict before deletion. Old stored generations
// are logically absent and do not increase the live removal count. Adapters share
// ValidateTaggedBatch; metadata and unrelated keys must remain untouched.
type TaggedBatchBackend interface {
	// ForgetManyTagged atomically removes the batch under its shared current snapshot.
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
	// GetMany returns owned read results in the canonical input order.
	GetMany(context.Context, []EntryKey) ([]BatchValue, error)
}

// TaggedBatchReadBackend reads a canonical batch sharing one tag snapshot (see
// ValidateTaggedBatch) atomically with its validation. A changed or missing
// snapshot returns Conflict; obsolete payloads are misses and may be reclaimed.
type TaggedBatchReadBackend interface {
	// GetManyTagged returns owned results in input order under one current tag snapshot.
	GetManyTagged(context.Context, []TaggedKey) ([]BatchValue, error)
}
