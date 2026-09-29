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
	parse      func(string) (F, error)
}

func DefineHash[K, F, V any](name Name, version Version, keys keyspace.Codec[K], fields keyspace.Codec[F]) HashDeclaration[K, F, V] {
	return HashDeclaration[K, F, V]{definition: &definition[K]{name: name, version: version, kind: HashKind, keys: keys}, fields: fields}
}

// WithFieldDecoder adds the inverse of the field codec, which Hash.GetAll needs
// to return typed fields. Every decoded field must encode back to the exact
// stored bytes. The returned declaration keeps the same name/version identity.
func (d HashDeclaration[K, F, V]) WithFieldDecoder(parse func(string) (F, error)) HashDeclaration[K, F, V] {
	d.parse = parse
	return d
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
	return Hash[K, F, V]{handle[K]{s, d.definition}, d.fields, d.parse, backend}, nil
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

// SortedSetDeclaration preserves resource and member types. Members use canonical
// JSON identity, as in sets, and each carries a float64 score.
type SortedSetDeclaration[K, V any] struct{ definition *definition[K] }

func DefineSortedSet[K, V any](name Name, version Version, keys keyspace.Codec[K]) SortedSetDeclaration[K, V] {
	return SortedSetDeclaration[K, V]{&definition[K]{name: name, version: version, kind: SortedSetKind, keys: keys}}
}
func (d SortedSetDeclaration[K, V]) Bind(s *Store) (SortedSet[K, V], error) {
	if err := d.definition.validate(); err != nil {
		return SortedSet[K, V]{}, err
	}
	if s == nil {
		return SortedSet[K, V]{}, fault.New(fault.Invalid, "sorted set binding requires a store")
	}
	backend, ok := s.backend.(SortedSetBackend)
	if !ok {
		return SortedSet[K, V]{}, fault.New(fault.Invalid, "adapter does not support Redis sorted sets")
	}
	if err := s.bind(d.definition.name, d.definition.version, &d.definition.id); err != nil {
		return SortedSet[K, V]{}, err
	}
	return SortedSet[K, V]{handle[K]{s, d.definition}, backend}, nil
}

// ListDeclaration preserves resource and element types. Elements are canonical
// JSON snapshots in insertion order; duplicates are allowed.
type ListDeclaration[K, V any] struct{ definition *definition[K] }

func DefineList[K, V any](name Name, version Version, keys keyspace.Codec[K]) ListDeclaration[K, V] {
	return ListDeclaration[K, V]{&definition[K]{name: name, version: version, kind: ListKind, keys: keys}}
}
func (d ListDeclaration[K, V]) Bind(s *Store) (List[K, V], error) {
	if err := d.definition.validate(); err != nil {
		return List[K, V]{}, err
	}
	if s == nil {
		return List[K, V]{}, fault.New(fault.Invalid, "list binding requires a store")
	}
	backend, ok := s.backend.(ListBackend)
	if !ok {
		return List[K, V]{}, fault.New(fault.Invalid, "adapter does not support Redis lists")
	}
	if err := s.bind(d.definition.name, d.definition.version, &d.definition.id); err != nil {
		return List[K, V]{}, err
	}
	return List[K, V]{handle[K]{s, d.definition}, backend}, nil
}
