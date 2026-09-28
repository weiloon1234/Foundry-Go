package pubsub

import (
	"github.com/weiloon1234/Foundry-Go/fault"
	"github.com/weiloon1234/Foundry-Go/keyspace"
)

type declarationID struct{ marker byte }
type declarationKey struct {
	name    Name
	version Version
}
type definition[K, V any] struct {
	id      declarationID
	name    Name
	version Version
	keys    keyspace.Codec[K]
}

// Declaration preserves both resource-key and JSON payload types. Increment Version
// for incompatible payload contracts; define once and reuse across callers.
type Declaration[K, V any] struct{ definition *definition[K, V] }

func Define[K, V any](name Name, version Version, keys keyspace.Codec[K]) Declaration[K, V] {
	return Declaration[K, V]{&definition[K, V]{name: name, version: version, keys: keys}}
}
func (d Declaration[K, V]) Name() Name {
	if d.definition == nil {
		return ""
	}
	return d.definition.name
}
func (d Declaration[K, V]) Version() Version {
	if d.definition == nil {
		return 0
	}
	return d.definition.version
}
func (d Declaration[K, V]) Validate() error {
	if d.definition == nil || !keyspace.ValidName(string(d.Name())) || d.Version() == 0 {
		return fault.New(fault.Invalid, "invalid pub/sub declaration")
	}
	return d.definition.keys.Validate()
}
func (d Declaration[K, V]) Bind(b *Broker) (Topic[K, V], error) {
	if err := d.Validate(); err != nil {
		return Topic[K, V]{}, err
	}
	if b == nil || b.done == nil {
		return Topic[K, V]{}, fault.New(fault.Invalid, "pub/sub binding requires a broker")
	}
	b.mu.Lock()
	defer b.mu.Unlock()
	if b.closing {
		return Topic[K, V]{}, ErrClosed
	}
	key := declarationKey{d.Name(), d.Version()}
	previous, exists := b.declarations[key]
	if exists && previous != &d.definition.id {
		return Topic[K, V]{}, fault.New(fault.Duplicate, "pub/sub topic already has another declaration")
	}
	if !exists {
		if len(b.declarations) >= b.config.MaxDeclarations {
			return Topic[K, V]{}, fault.New(fault.Invalid, "pub/sub declaration capacity reached")
		}
		b.declarations[key] = &d.definition.id
	}
	return Topic[K, V]{b, d.definition}, nil
}
