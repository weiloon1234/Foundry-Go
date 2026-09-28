package raw

import (
	"context"
	"strconv"

	"github.com/weiloon1234/Foundry-Go/fault"
	"github.com/weiloon1234/Foundry-Go/internal/keyaddress"
	"github.com/weiloon1234/Foundry-Go/keyspace"
	"github.com/weiloon1234/Foundry-Go/redis/data"
)

type Name string
type Version uint32

// Key is an explicit adapter address. Obtain it from Keys.For or FromDataKey.
// Possessing a key does not statically verify arbitrary command/script arguments.
type Key struct {
	namespace keyspace.Namespace
	text      string
}

func NewKey(ns keyspace.Namespace, name Name, version Version, logical string) (Key, error) {
	if version == 0 {
		return Key{}, fault.New(fault.Invalid, "raw Redis key requires a version")
	}
	a, err := keyaddress.New(ns, string(name), logical)
	if err != nil {
		return Key{}, err
	}
	return Key{ns, a.String("raw") + ":" + strconv.FormatUint(uint64(version), 10)}, nil
}
func FromDataKey(key data.Key) (Key, error) {
	if err := key.Validate(); err != nil {
		return Key{}, err
	}
	return Key{key.Namespace(), key.String()}, nil
}
func (k Key) Validate() error {
	if k.text == "" {
		return fault.New(fault.Invalid, "raw Redis key is uninitialized")
	}
	return k.namespace.Validate()
}
func (k Key) String() string                { return k.text }
func (k Key) Namespace() keyspace.Namespace { return k.namespace }

type declarationID struct{ marker byte }
type declarationKey struct {
	name    Name
	version Version
}
type definition[K any] struct {
	id      declarationID
	name    Name
	version Version
	codec   keyspace.Codec[K]
}
type Declaration[K any] struct{ definition *definition[K] }

func DefineKeys[K any](name Name, version Version, codec keyspace.Codec[K]) Declaration[K] {
	return Declaration[K]{&definition[K]{name: name, version: version, codec: codec}}
}
func (d Declaration[K]) Bind(s *Store) (Keys[K], error) {
	if d.definition == nil || !keyspace.ValidName(string(d.definition.name)) || d.definition.version == 0 {
		return Keys[K]{}, fault.New(fault.Invalid, "invalid raw Redis key declaration")
	}
	if err := d.definition.codec.Validate(); err != nil {
		return Keys[K]{}, err
	}
	if err := s.bind(d.definition.name, d.definition.version, &d.definition.id); err != nil {
		return Keys[K]{}, err
	}
	return Keys[K]{s, d.definition}, nil
}

// Keys preserves the domain resource type while resolving explicit raw addresses.
type Keys[K any] struct {
	store      *Store
	definition *definition[K]
}

func (k Keys[K]) resolve(ctx context.Context, input K) (Key, error) {
	if k.definition == nil || k.store == nil {
		return Key{}, fault.New(fault.Invalid, "raw Redis keys are unbound")
	}
	text, err := k.definition.codec.Encode(input)
	if err != nil {
		return Key{}, err
	}
	if err := ctx.Err(); err != nil {
		return Key{}, err
	}
	if len(text) > k.store.config.MaxKeyBytes {
		return Key{}, fault.New(fault.Invalid, "raw Redis logical key exceeds its bound")
	}
	return NewKey(k.store.config.Namespace, k.definition.name, k.definition.version, text)
}
func (k Keys[K]) For(ctx context.Context, input K) (Key, error) {
	var key Key
	err := k.store.execute(ctx, func(ctx context.Context) error { var err error; key, err = k.resolve(ctx, input); return err })
	if err != nil {
		return Key{}, err
	}
	return key, nil
}

func (k Keys[K]) Name() Name {
	if k.definition == nil {
		return ""
	}
	return k.definition.name
}
func (k Keys[K]) Version() Version {
	if k.definition == nil {
		return 0
	}
	return k.definition.version
}
