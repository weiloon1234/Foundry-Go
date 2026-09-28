package lockout

import (
	"github.com/weiloon1234/Foundry-Go/fault"
	"github.com/weiloon1234/Foundry-Go/keyspace"
)

type declarationID struct{ marker byte }
type definition[K any] struct {
	id     declarationID
	name   Name
	codec  keyspace.Codec[K]
	policy Policy
}

// Declaration retains the submitted login key type. Include tenant/provider
// identity in its codec or family name, and use the same normalization as lookup.
// Missing and existing accounts must use the same policy and key construction.
type Declaration[K any] struct{ definition *definition[K] }

func Define[K any](name Name, codec keyspace.Codec[K], policy Policy) Declaration[K] {
	return Declaration[K]{&definition[K]{name: name, codec: codec, policy: policy}}
}
func (d Declaration[K]) Name() Name {
	if d.definition == nil {
		return ""
	}
	return d.definition.name
}
func (d Declaration[K]) Policy() Policy {
	if d.definition == nil {
		return Policy{}
	}
	return d.definition.policy
}
func (d Declaration[K]) Validate() error {
	if d.definition == nil || !keyspace.ValidName(string(d.Name())) {
		return fault.New(fault.Invalid, "invalid lockout declaration")
	}
	if err := d.definition.codec.Validate(); err != nil {
		return err
	}
	return d.Policy().Validate()
}
func (d Declaration[K]) Bind(store *Store) (Throttle[K], error) {
	if err := d.Validate(); err != nil {
		return Throttle[K]{}, err
	}
	if store == nil || store.gate == nil {
		return Throttle[K]{}, fault.New(fault.Invalid, "lockout binding requires a store")
	}
	store.mu.Lock()
	defer store.mu.Unlock()
	id, exists := store.declarations[d.Name()]
	if exists && id != &d.definition.id {
		return Throttle[K]{}, fault.New(fault.Duplicate, "lockout family has a different declaration")
	}
	if !exists {
		if len(store.declarations) >= store.config.MaxDeclarations {
			return Throttle[K]{}, fault.New(fault.Invalid, "lockout declaration capacity reached")
		}
		store.declarations[d.Name()] = &d.definition.id
	}
	return Throttle[K]{store: store, definition: d.definition}, nil
}
