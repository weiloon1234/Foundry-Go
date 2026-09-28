package ratelimit

import (
	"context"
	"github.com/weiloon1234/Foundry-Go/fault"
	"github.com/weiloon1234/Foundry-Go/internal/keyaddress"
	"github.com/weiloon1234/Foundry-Go/keyspace"
)

type Name string

// Key is an opaque adapter address, distinct from cache and lease keys.
type Key struct{ address keyaddress.Address }

// NewKey is for adapters and tests. Applications use a typed Limiter.
func NewKey(namespace keyspace.Namespace, name Name, logical string) (Key, error) {
	address, err := keyaddress.New(namespace, string(name), logical)
	return Key{address}, err
}
func (k Key) Validate() error               { return k.address.Validate() }
func (k Key) Namespace() keyspace.Namespace { return k.address.Namespace }
func (k Key) String() string                { return k.address.String("ratelimit") }

// Backend owns one authority's clock and atomic fixed-window decisions. Live
// buckets reject incompatible policies with fault.Conflict. Denials do not mutate
// usage. Implementations honor cancellation, bound metadata reads and never
// implicitly retry a mutation or fall back to another authority. The caller owns
// the adapter lifecycle; network errors may leave consumption unconfirmed.
type Backend interface {
	RateLimit(context.Context, Key, Limit, uint32) (Decision, error)
}

func ValidateOperation(ctx context.Context, key Key, limit Limit, cost uint32) error {
	if ctx == nil {
		return fault.New(fault.Invalid, "rate limiting requires a context")
	}
	if err := ctx.Err(); err != nil {
		return err
	}
	if err := key.Validate(); err != nil {
		return err
	}
	return limit.ValidateCost(cost)
}
