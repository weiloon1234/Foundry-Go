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
	DataExists(context.Context, Key) (bool, error)
	DataExpire(context.Context, Key, cache.TTL) (bool, error)
	DataDeleteMany(context.Context, []Key) (uint64, error)
	DataCount(context.Context, Key, Limits) (uint64, error)
}

// HashBackend supplies point operations. Get distinguishes missing from JSON
// null. Set returns true only for a new field. Writes retain existing key TTL;
// a newly created key is persistent. Deleting the last field removes the key.
// Adapters validate kind, cardinality and requested field/value sizes before I/O
// or mutation. Stored values are opaque bytes; typed handles validate their JSON.
type HashBackend interface {
	HashGet(context.Context, Key, string, Limits) (string, bool, error)
	HashSet(context.Context, Key, string, string, Limits) (bool, error)
	HashDelete(context.Context, Key, string, Limits) (bool, error)
}

// SetBackend uses exact encoded member equality. Writes preserve TTL, new sets
// are persistent, and removing the last member removes the key. Members returns
// sorted, unique, owned strings within the bounds, including an empty slice for
// missing sets. Every method validates type and cardinality before mutation.
type SetBackend interface {
	SetAdd(context.Context, Key, string, Limits) (bool, error)
	SetRemove(context.Context, Key, string, Limits) (bool, error)
	SetContains(context.Context, Key, string, Limits) (bool, error)
	SetMembers(context.Context, Key, Limits) ([]string, error)
}

// StoredField is one adapter hash field and its encoded value.
type StoredField struct{ Field, Value string }

// HashReadBackend optionally reads several fields in one operation. GetMany
// returns one entry per requested field, in order; a missing field is unset.
// GetAll returns every field sorted by field bytes after checking cardinality
// and the reply budget before transferring payloads.
type HashReadBackend interface {
	HashGetMany(context.Context, Key, []string, Limits) ([]value.Optional[string], error)
	HashGetAll(context.Context, Key, Limits) ([]StoredField, error)
}

// HashCounterBackend optionally adds an exact signed delta to one integer field.
// A missing field starts at zero. Non-integer values, overflow and growth past
// the cardinality bound fail without mutation. Writes retain the key's expiry.
type HashCounterBackend interface {
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
	SortedSetAdd(context.Context, Key, string, float64, Limits) (bool, error)
	SortedSetIncrement(context.Context, Key, string, float64, Limits) (float64, error)
	SortedSetRemove(context.Context, Key, string, Limits) (bool, error)
	SortedSetScore(context.Context, Key, string, Limits) (float64, bool, error)
	SortedSetRank(context.Context, Key, string, Order, Limits) (uint64, bool, error)
	SortedSetRange(context.Context, Key, Window, Limits) ([]StoredMember, error)
	SortedSetRangeByScore(context.Context, Key, ScoreRange, Window, Limits) ([]StoredMember, error)
	SortedSetCountByScore(context.Context, Key, ScoreRange, Limits) (uint64, error)
}

// ListBackend supplies bounded list operations. Push checks the resulting length
// against Limits.Entries before inserting anything and returns the new length.
// Pop checks the element's size before removing it. Range/Trim use Redis index
// semantics (negative indices count from the tail). Writes preserve TTL, new
// lists are persistent and removing the last element removes the key.
type ListBackend interface {
	ListPush(context.Context, Key, []string, End, Limits) (uint64, error)
	ListPop(context.Context, Key, End, Limits) (string, bool, error)
	ListRange(context.Context, Key, int64, int64, Limits) ([]string, error)
	ListTrim(context.Context, Key, int64, int64, Limits) error
}
