package lockout

import (
	"context"

	"github.com/weiloon1234/Foundry-Go/fault"
	"github.com/weiloon1234/Foundry-Go/internal/keyaddress"
	"github.com/weiloon1234/Foundry-Go/internal/safeerror"
	"github.com/weiloon1234/Foundry-Go/keyspace"
)

// Name identifies a lockout declaration family.
type Name string

// Key is an adapter address, distinct from cache, lease and request-limit keys.
// Logical identifiers are hashed by the shared address codec, not logged raw.
type Key struct{ address keyaddress.Address }

// NewKey validates and constructs an adapter address for namespace, name and logical
// key.
func NewKey(namespace keyspace.Namespace, name Name, logical string) (Key, error) {
	address, err := keyaddress.New(namespace, string(name), logical)
	return Key{address}, err
}

// Validate checks the adapter address.
func (k Key) Validate() error { return k.address.Validate() }

// Namespace returns the key's application namespace.
func (k Key) Namespace() keyspace.Namespace { return k.address.Namespace }

// String returns the physical lockout address.
func (k Key) String() string { return k.address.String(KeyFeature) }

// ValidateOperation checks context cancellation, key validity and lockout policy.
func ValidateOperation(ctx context.Context, key Key, policy Policy) error {
	if ctx == nil {
		return safeerror.NewFault(fault.Invalid, "lockout requires a context")
	}
	if err := ctx.Err(); err != nil {
		return err
	}
	if err := key.Validate(); err != nil {
		return err
	}
	return policy.Validate()
}

// KeyFeature is the physical address family owned by this adapter.
const KeyFeature keyspace.Feature = "lockout"
