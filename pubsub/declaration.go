package pubsub

import (
	"github.com/weiloon1234/Foundry-Go/fault"
	"github.com/weiloon1234/Foundry-Go/internal/safeerror"
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

// Define constructs a typed, versioned topic family using keys to encode resource keys.
func Define[K, V any](name Name, version Version, keys keyspace.Codec[K]) Declaration[K, V] {
	return Declaration[K, V]{&definition[K, V]{name: name, version: version, keys: keys}}
}

// Name returns the topic family name, or an empty name for a zero declaration.
func (d Declaration[K, V]) Name() Name {
	if d.definition == nil {
		return ""
	}
	return d.definition.name
}

// Version returns the schema version, or zero for a zero declaration.
func (d Declaration[K, V]) Version() Version {
	if d.definition == nil {
		return 0
	}
	return d.definition.version
}

// Validate checks the name, positive schema version, and resource key codec.
func (d Declaration[K, V]) Validate() error {
	if d.definition == nil || !keyspace.ValidName(string(d.Name())) || d.Version() == 0 {
		return safeerror.NewFault(fault.Invalid, "invalid pub/sub declaration")
	}
	return d.definition.keys.Validate()
}

// Bind registers this declaration with b and returns a typed topic.
// A different declaration with the same name and version fails with fault.Duplicate.
func (d Declaration[K, V]) Bind(b *Broker) (Topic[K, V], error) {
	if err := d.Validate(); err != nil {
		return Topic[K, V]{}, err
	}
	if b == nil || b.done == nil {
		return Topic[K, V]{}, safeerror.NewFault(fault.Invalid, "pub/sub binding requires a broker")
	}
	b.mu.Lock()
	defer b.mu.Unlock()
	if b.closing {
		return Topic[K, V]{}, ErrClosed
	}
	key := declarationKey{d.Name(), d.Version()}
	previous, exists := b.declarations[key]
	if exists && previous != &d.definition.id {
		return Topic[K, V]{}, safeerror.NewFault(fault.Duplicate, "pub/sub topic already has another declaration")
	}
	if !exists {
		if len(b.declarations) >= b.config.MaxDeclarations {
			return Topic[K, V]{}, safeerror.NewFault(fault.Invalid, "pub/sub declaration capacity reached")
		}
		b.declarations[key] = &d.definition.id
	}
	return Topic[K, V]{b, d.definition}, nil
}
