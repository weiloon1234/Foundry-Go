package lockout

import (
	"context"

	"github.com/weiloon1234/Foundry-Go/fault"
	"github.com/weiloon1234/Foundry-Go/internal/keyaddress"
	"github.com/weiloon1234/Foundry-Go/keyspace"
)

type Name string

// Key is an adapter address, distinct from cache, lease and request-limit keys.
// Logical identifiers are hashed by the shared address codec, not logged raw.
type Key struct{ address keyaddress.Address }

func NewKey(namespace keyspace.Namespace, name Name, logical string) (Key, error) {
	address, err := keyaddress.New(namespace, string(name), logical)
	return Key{address}, err
}
func (k Key) Validate() error               { return k.address.Validate() }
func (k Key) Namespace() keyspace.Namespace { return k.address.Namespace }
func (k Key) String() string                { return k.address.String("lockout") }
func ValidateOperation(ctx context.Context, key Key, policy Policy) error {
	if ctx == nil {
		return fault.New(fault.Invalid, "lockout requires a context")
	}
	if err := ctx.Err(); err != nil {
		return err
	}
	if err := key.Validate(); err != nil {
		return err
	}
	return policy.Validate()
}
