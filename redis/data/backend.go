package data

import (
	"context"

	"github.com/weiloon1234/Foundry-Go/cache"
	"github.com/weiloon1234/Foundry-Go/value"
)

// Backend owns data entry operations. Validate every key and complete batch
// before mutation. DeleteMany is one atomic operation; error returns zero and
// may hide a completed remote write. Never retry uncertain mutations. The caller
// owns adapter lifecycle; stores borrow it. Expire preserves bytes, missing keys
// return false, and Forever returns true even for an already persistent key.
type Backend interface {
	// DataExists reports whether the declared data key exists.
	DataExists(context.Context, Key) (bool, error)
	// DataExpire changes an existing key's TTL without changing its contents.
	DataExpire(context.Context, Key, cache.TTL) (bool, error)
	// DataDeleteMany atomically deletes a canonical batch and returns the number of removed keys.
	DataDeleteMany(context.Context, []Key) (uint64, error)
	// DataCount returns collection cardinality within the supplied limits.
	DataCount(context.Context, Key, Limits) (uint64, error)
}

// HashBackend supplies point operations. Get distinguishes missing from JSON
// null. Set returns true only for a new field. Writes retain existing key TTL;
// a newly created key is persistent. Deleting the last field removes the key.
// Adapters validate kind, cardinality and requested field/value sizes before I/O
// or mutation. Stored values are opaque bytes; typed handles validate their JSON.
type HashBackend interface {
	// HashGet returns an encoded field value and distinguishes a missing field.
	HashGet(context.Context, Key, string, Limits) (string, bool, error)
	// HashSet stores an encoded field value and reports whether the field was newly inserted.
	HashSet(context.Context, Key, string, string, Limits) (bool, error)
	// HashDelete removes a field and reports whether it existed.
	HashDelete(context.Context, Key, string, Limits) (bool, error)
}

// SetBackend uses exact encoded member equality. Writes preserve TTL, new sets
// are persistent, and removing the last member removes the key. Members returns
// sorted, unique, owned strings within the bounds, including an empty slice for
// missing sets. Every method validates type and cardinality before mutation.
type SetBackend interface {
	// SetAdd inserts an encoded member and reports whether it was new.
	SetAdd(context.Context, Key, string, Limits) (bool, error)
	// SetRemove removes an encoded member and reports whether it existed.
	SetRemove(context.Context, Key, string, Limits) (bool, error)
	// SetContains tests exact encoded member equality.
	SetContains(context.Context, Key, string, Limits) (bool, error)
	// SetMembers returns owned member strings in byte order within the reply limits.
	SetMembers(context.Context, Key, Limits) ([]string, error)
}

// StoredField is one adapter hash field and its encoded value.
type StoredField struct{ Field, Value string }

// HashReadBackend optionally reads several fields in one operation. GetMany
// returns one entry per requested field, in order; a missing field is unset.
// GetAll returns every field sorted by field bytes after checking cardinality
// and the reply budget before transferring payloads.
type HashReadBackend interface {
	// HashGetMany returns optional encoded values in requested field order.
	HashGetMany(context.Context, Key, []string, Limits) ([]value.Optional[string], error)
	// HashGetAll returns every encoded field and value ordered by field bytes.
	HashGetAll(context.Context, Key, Limits) ([]StoredField, error)
}

// HashCounterBackend optionally adds an exact signed delta to one integer field.
// A missing field starts at zero. Non-integer values, overflow and growth past
// the cardinality bound fail without mutation. Writes retain the key's expiry.
type HashCounterBackend interface {
	// HashIncrement atomically adds delta to an integer field, starting a missing field at zero.
	HashIncrement(context.Context, Key, string, int64, Limits) (int64, error)
}

// StoredMember is one adapter sorted-set member with its score.
type StoredMember struct {
	Member string
	Score  float64
}

// SortedSetBackend orders members by float64 score, then by member bytes, as
// Redis does. Scores are never NaN. Writes preserve TTL, new sorted sets are
// persistent and removing the last member removes the key. Range replies are
// bounded by Window.Count, Limits.Entries and Limits.ReplyBytes before payloads
// are returned. Every method validates type and cardinality before mutation.
type SortedSetBackend interface {
	// SortedSetAdd inserts or re-scores a member and reports whether it was new.
	SortedSetAdd(context.Context, Key, string, float64, Limits) (bool, error)
	// SortedSetIncrement atomically adds delta to a member's score, starting a missing member at zero.
	SortedSetIncrement(context.Context, Key, string, float64, Limits) (float64, error)
	// SortedSetRemove removes a member and reports whether it existed.
	SortedSetRemove(context.Context, Key, string, Limits) (bool, error)
	// SortedSetScore returns a member's score and distinguishes absence.
	SortedSetScore(context.Context, Key, string, Limits) (float64, bool, error)
	// SortedSetRank returns a member's zero-based rank in the selected order and distinguishes absence.
	SortedSetRank(context.Context, Key, string, Order, Limits) (uint64, bool, error)
	// SortedSetRange returns members and scores within a bounded rank window.
	SortedSetRange(context.Context, Key, Window, Limits) ([]StoredMember, error)
	// SortedSetRangeByScore returns matching members and scores within a bounded window.
	SortedSetRangeByScore(context.Context, Key, ScoreRange, Window, Limits) ([]StoredMember, error)
	// SortedSetCountByScore counts members matching the score range.
	SortedSetCountByScore(context.Context, Key, ScoreRange, Limits) (uint64, error)
}

// ListBackend supplies bounded list operations. Push checks the resulting length
// against Limits.Entries before inserting anything and returns the new length.
// Pop checks the element's size before removing it. Range/Trim use Redis index
// semantics (negative indices count from the tail). Writes preserve TTL, new
// lists are persistent and removing the last element removes the key.
type ListBackend interface {
	// ListPush inserts values at the selected end and returns the new length.
	ListPush(context.Context, Key, []string, End, Limits) (uint64, error)
	// ListPop removes a size-checked element from the selected end and distinguishes absence.
	ListPop(context.Context, Key, End, Limits) (string, bool, error)
	// ListRange returns elements from start through stop inclusive, with negative indices relative to the tail.
	ListRange(context.Context, Key, int64, int64, Limits) ([]string, error)
	// ListTrim retains elements from start through stop inclusive, with negative indices relative to the tail.
	ListTrim(context.Context, Key, int64, int64, Limits) error
}
