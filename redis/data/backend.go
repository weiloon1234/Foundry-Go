package data

import (
	"context"

	"github.com/weiloon1234/Foundry-Go/cache"
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
