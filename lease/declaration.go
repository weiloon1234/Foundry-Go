package lease

import (
	"github.com/weiloon1234/Foundry-Go/fault"
	"github.com/weiloon1234/Foundry-Go/keyspace"
)

type declarationID struct{ marker byte }
type definition[K any] struct {
	id    declarationID
	name  Name
	codec keyspace.Codec[K]
}

// Declaration retains the resource key type. Reuse one declared value per family.
type Declaration[K any] struct{ definition *definition[K] }

func Define[K any](name Name, codec keyspace.Codec[K]) Declaration[K] {
	return Declaration[K]{definition: &definition[K]{name: name, codec: codec}}
}
func (d Declaration[K]) Name() Name {
	if d.definition == nil {
		return ""
	}
	return d.definition.name
}
func (d Declaration[K]) Validate() error {
	if d.definition == nil || !keyspace.ValidName(string(d.Name())) {
		return fault.New(fault.Invalid, "invalid lease declaration")
	}
	return d.definition.codec.Validate()
}

// Bind rejects a distinct declaration with the same family name, even for the same K.
func (d Declaration[K]) Bind(m *Manager) (Leases[K], error) {
	if err := d.Validate(); err != nil {
		return Leases[K]{}, err
	}
	if m == nil || m.done == nil {
		return Leases[K]{}, fault.New(fault.Invalid, "lease binding requires a manager")
	}
	m.mu.Lock()
	defer m.mu.Unlock()
	if m.closing {
		return Leases[K]{}, fault.New(fault.Closed, "lease manager is closed")
	}
	id, exists := m.declarations[d.Name()]
	if exists && id != &d.definition.id {
		return Leases[K]{}, fault.New(fault.Duplicate, "lease family already has a different declaration")
	}
	if !exists {
		if len(m.declarations) >= m.config.MaxDeclarations {
			return Leases[K]{}, fault.New(fault.Invalid, "lease declaration limit exceeded")
		}
		m.declarations[d.Name()] = &d.definition.id
	}
	return Leases[K]{manager: m, definition: d.definition}, nil
}

// Leases only accepts its declaration's concrete resource key type.
type Leases[K any] struct {
	manager    *Manager
	definition *definition[K]
}
