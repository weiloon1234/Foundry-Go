// Package metadata stores typed, model-owned values through explicit key
// declarations. It shares owner identity and transactions with model extensions.
package metadata

import (
	"fmt"

	"github.com/weiloon1234/Foundry-Go/contract"
	"github.com/weiloon1234/Foundry-Go/extensions"
	"github.com/weiloon1234/Foundry-Go/fault"
	"github.com/weiloon1234/Foundry-Go/internal/extensionvalue"
	"github.com/weiloon1234/Foundry-Go/internal/identifier"
)

type Name string
type Version uint32

const MaxValueBytes = extensionvalue.MaxBytes
const MaxBatchBytes = extensionvalue.MaxBatchBytes
const MaxKeysPerOwner = 256

type declarationID struct{ nonzero byte }

// Key retains both the owner model/key and the value codec. Reuse the same
// declaration in assembly and calls; a matching string name cannot substitute
// an unrelated value type or schema. Increment Version for incompatible changes.
type Key[M any, K comparable, V any] struct{ definition *definition[M, K, V] }
type definition[M any, K comparable, V any] struct {
	owner   extensions.Owner[M, K]
	name    Name
	version Version
	value   contract.JSON[V]
	id      *declarationID
}

func Define[M any, K comparable, V any](owner extensions.Owner[M, K], name Name, version Version, schema contract.JSON[V]) Key[M, K, V] {
	return Key[M, K, V]{definition: &definition[M, K, V]{owner: owner, name: name, version: version, value: schema, id: &declarationID{}}}
}
func (k Key[M, K, V]) Validate() error {
	if k.definition == nil || !identifier.Semantic(string(k.definition.name)) || k.definition.version == 0 {
		return invalid()
	}
	if err := k.definition.owner.Validate(); err != nil {
		return err
	}
	return k.definition.value.Validate()
}
func (k Key[M, K, V]) Name() Name {
	if k.definition == nil {
		return ""
	}
	return k.definition.name
}
func (k Key[M, K, V]) Version() Version {
	if k.definition == nil {
		return 0
	}
	return k.definition.version
}
func (Key[M, K, V]) Format(s fmt.State, _ rune) { _, _ = s.Write([]byte("typed metadata key")) }
func (k Key[M, K, V]) registrationKey() string {
	if k.definition == nil {
		return ""
	}
	return extensions.Digest(k.definition.owner.Scope(), string(k.Name()))
}

type Registration struct {
	key      string
	id       *declarationID
	validate func(*extensions.Registry) error
}

func (k Key[M, K, V]) Registration() Registration {
	if k.definition == nil {
		return Registration{}
	}
	return Registration{key: k.registrationKey(), id: k.definition.id, validate: func(owners *extensions.Registry) error {
		if err := k.Validate(); err != nil {
			return err
		}
		return k.definition.owner.Check(owners)
	}}
}

// Manager borrows the common Store; it has no independent background work or
// shutdown. The store bounds and owns every metadata operation's actual lifetime.
type Manager struct {
	store *extensions.Store
	keys  map[string]*declarationID
}

func New(store *extensions.Store, keys ...Registration) (*Manager, error) {
	if err := store.Validate(); err != nil {
		return nil, err
	}
	if len(keys) > 4096 {
		return nil, invalid()
	}
	m := &Manager{store: store, keys: make(map[string]*declarationID, len(keys))}
	for _, key := range keys {
		if key.id == nil || key.validate == nil {
			return nil, invalid()
		}
		if err := key.validate(store.Registry()); err != nil {
			return nil, err
		}
		if _, ok := m.keys[key.key]; ok {
			return nil, fault.New(fault.Duplicate, "metadata key already registered")
		}
		m.keys[key.key] = key.id
	}
	return m, nil
}
func (m *Manager) Validate() error {
	if m == nil || m.keys == nil {
		return invalid()
	}
	return m.store.Validate()
}
func (k Key[M, K, V]) check(m *Manager) error {
	if err := k.Validate(); err != nil {
		return err
	}
	if err := m.Validate(); err != nil {
		return err
	}
	if m.keys[k.registrationKey()] != k.definition.id {
		return invalid()
	}
	return k.definition.owner.Check(m.store.Registry())
}
func invalid() error { return fault.New(fault.Invalid, "invalid metadata declaration or data") }
