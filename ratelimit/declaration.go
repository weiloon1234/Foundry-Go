package ratelimit

import (
	"github.com/weiloon1234/Foundry-Go/fault"
	"github.com/weiloon1234/Foundry-Go/keyspace"
)

type declarationID struct{ marker byte }
type definition[K any] struct {
	id    declarationID
	name  Name
	codec keyspace.Codec[K]
	limit Limit
}

// Declaration preserves its resource key type and immutable policy. Reuse one
// declaration for a shared quota; distinct names intentionally isolate quotas.
type Declaration[K any] struct{ definition *definition[K] }

func Define[K any](name Name, codec keyspace.Codec[K], limit Limit) Declaration[K] {
	return Declaration[K]{&definition[K]{name: name, codec: codec, limit: limit}}
}
func (d Declaration[K]) Name() Name {
	if d.definition == nil {
		return ""
	}
	return d.definition.name
}
func (d Declaration[K]) Limit() Limit {
	if d.definition == nil {
		return Limit{}
	}
	return d.definition.limit
}
func (d Declaration[K]) Validate() error {
	if d.definition == nil || !keyspace.ValidName(string(d.Name())) {
		return fault.New(fault.Invalid, "invalid rate limit declaration")
	}
	if err := d.definition.codec.Validate(); err != nil {
		return err
	}
	return d.Limit().Validate()
}

// Bind rejects a different declaration with the same name, even with identical K.
func (d Declaration[K]) Bind(store *Store) (Limiter[K], error) {
	if err := d.Validate(); err != nil {
		return Limiter[K]{}, err
	}
	if store == nil || store.backend == nil {
		return Limiter[K]{}, fault.New(fault.Invalid, "rate limit binding requires a store")
	}
	store.mu.Lock()
	defer store.mu.Unlock()
	id, exists := store.declarations[d.Name()]
	if exists && id != &d.definition.id {
		return Limiter[K]{}, fault.New(fault.Duplicate, "rate limit family already has a different declaration")
	}
	if !exists {
		if len(store.declarations) >= store.config.MaxDeclarations {
			return Limiter[K]{}, fault.New(fault.Invalid, "rate limit declaration capacity reached")
		}
		store.declarations[d.Name()] = &d.definition.id
	}
	return Limiter[K]{store, d.definition}, nil
}
