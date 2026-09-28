package cache

import (
	"github.com/weiloon1234/Foundry-Go/internal/keyaddress"
	"github.com/weiloon1234/Foundry-Go/keyspace"
)

// Name identifies a declared cache family within an application/environment.
type Name string

// Namespace separates applications and environments sharing a backend.
type Namespace = keyspace.Namespace

const MaxKeyBytes = keyspace.MaxKeyBytes

func validName(name string) bool { return keyspace.ValidName(name) }

// EntryKey is the explicit backend address boundary. Adapters must retain its
// complete namespace. Applications use their typed Cache to construct addresses.
type EntryKey struct {
	address      keyaddress.Address
	namespaceTag bool
}

// NewEntryKey is intended for adapter authors. Codecs must distinguish semantic keys.
func NewEntryKey(namespace Namespace, name Name, logical string) (EntryKey, error) {
	address, err := keyaddress.New(namespace, string(name), logical)
	return EntryKey{address: address}, err
}
func (k EntryKey) Validate() error      { return k.address.Validate() }
func (k EntryKey) Namespace() Namespace { return k.address.Namespace }
func (k EntryKey) Name() Name           { return Name(k.address.Name) }
func (k EntryKey) String() string {
	if k.namespaceTag {
		return k.address.String("cache-namespace")
	}
	return k.address.String("cache")
}

// NewNamespaceTagKey returns the reserved adapter metadata address for a complete
// cache namespace. Its feature prefix cannot collide with application cache/tag
// declarations. Applications call Store.Invalidate instead of managing versions.
func NewNamespaceTagKey(namespace Namespace) (EntryKey, error) {
	key, err := NewEntryKey(namespace, "generation", "v1")
	key.namespaceTag = true
	return key, err
}
