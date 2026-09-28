package cache

import "github.com/weiloon1234/Foundry-Go/fault"

// A non-zero-size identity distinguishes declarations with the same name even
// when they happen to use the same Go types. Reuse the declared value to bind again.
type declarationID struct{ marker byte }
type definition[K, V any] struct {
	id     declarationID
	name   Name
	keys   KeyCodec[K]
	values Codec[V]
}

// Declaration is an immutable, reusable typed cache family. Its zero value is invalid.
type Declaration[K, V any] struct{ definition *definition[K, V] }

func Define[K, V any](name Name, keys KeyCodec[K], values Codec[V]) Declaration[K, V] {
	return Declaration[K, V]{definition: &definition[K, V]{name: name, keys: keys, values: values}}
}
func (d Declaration[K, V]) Name() Name {
	if d.definition == nil {
		return ""
	}
	return d.definition.name
}
func (d Declaration[K, V]) Validate() error {
	if d.definition == nil || !validName(string(d.definition.name)) {
		return fault.New(fault.Invalid, "invalid cache declaration")
	}
	if err := d.definition.keys.Validate(); err != nil {
		return err
	}
	return d.definition.values.Validate()
}

// Bind rejects conflicting declarations before they can read each other's data.
// Copies of the same declaration can bind repeatedly. Separate declarations with
// the same name fail, including declarations whose payload types happen to match.
func (d Declaration[K, V]) Bind(store *Store) (Cache[K, V], error) {
	if err := d.Validate(); err != nil {
		return Cache[K, V]{}, err
	}
	if store == nil || store.backend == nil {
		return Cache[K, V]{}, fault.New(fault.Invalid, "cache declaration requires an initialized store")
	}
	store.mu.Lock()
	defer store.mu.Unlock()
	id, exists := store.declarations[d.Name()]
	if exists && id != &d.definition.id {
		return Cache[K, V]{}, fault.New(fault.Duplicate, "cache family already has a different declaration")
	}
	if !exists {
		if len(store.declarations) >= store.config.MaxDeclarations {
			return Cache[K, V]{}, fault.New(fault.Invalid, "cache declaration limit exceeded")
		}
		store.declarations[d.Name()] = &d.definition.id
	}
	return Cache[K, V]{store: store, definition: d.definition}, nil
}
