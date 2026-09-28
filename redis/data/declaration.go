package data

import (
	"github.com/weiloon1234/Foundry-Go/fault"
	"github.com/weiloon1234/Foundry-Go/keyspace"
)

type definition[K any] struct {
	id      declarationID
	name    Name
	version Version
	kind    Kind
	keys    keyspace.Codec[K]
}

func (d *definition[K]) validate() error {
	if d == nil || !keyspace.ValidName(string(d.name)) || d.version == 0 {
		return fault.New(fault.Invalid, "invalid Redis data declaration")
	}
	return d.keys.Validate()
}

// HashDeclaration preserves the resource key, field identity and value types.
// Keep one declaration per name/version; increment version for schema changes.
type HashDeclaration[K, F, V any] struct {
	definition *definition[K]
	fields     keyspace.Codec[F]
}

func DefineHash[K, F, V any](name Name, version Version, keys keyspace.Codec[K], fields keyspace.Codec[F]) HashDeclaration[K, F, V] {
	return HashDeclaration[K, F, V]{&definition[K]{name: name, version: version, kind: HashKind, keys: keys}, fields}
}
func (d HashDeclaration[K, F, V]) Bind(s *Store) (Hash[K, F, V], error) {
	if err := d.definition.validate(); err != nil {
		return Hash[K, F, V]{}, err
	}
	if err := d.fields.Validate(); err != nil {
		return Hash[K, F, V]{}, err
	}
	if s == nil {
		return Hash[K, F, V]{}, fault.New(fault.Invalid, "hash binding requires a store")
	}
	backend, ok := s.backend.(HashBackend)
	if !ok {
		return Hash[K, F, V]{}, fault.New(fault.Invalid, "adapter does not support Redis hashes")
	}
	if err := s.bind(d.definition.name, d.definition.version, &d.definition.id); err != nil {
		return Hash[K, F, V]{}, err
	}
	return Hash[K, F, V]{handle[K]{s, d.definition}, d.fields, backend}, nil
}

// SetDeclaration preserves resource and member types. Members use canonical JSON
// equality, rather than Go pointer identity or the spelling of numeric JSON.
type SetDeclaration[K, V any] struct{ definition *definition[K] }

func DefineSet[K, V any](name Name, version Version, keys keyspace.Codec[K]) SetDeclaration[K, V] {
	return SetDeclaration[K, V]{&definition[K]{name: name, version: version, kind: SetKind, keys: keys}}
}
func (d SetDeclaration[K, V]) Bind(s *Store) (Set[K, V], error) {
	if err := d.definition.validate(); err != nil {
		return Set[K, V]{}, err
	}
	if s == nil {
		return Set[K, V]{}, fault.New(fault.Invalid, "set binding requires a store")
	}
	backend, ok := s.backend.(SetBackend)
	if !ok {
		return Set[K, V]{}, fault.New(fault.Invalid, "adapter does not support Redis sets")
	}
	if err := s.bind(d.definition.name, d.definition.version, &d.definition.id); err != nil {
		return Set[K, V]{}, err
	}
	return Set[K, V]{handle[K]{s, d.definition}, backend}, nil
}
