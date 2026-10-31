package jobs

import (
	"github.com/weiloon1234/Foundry-Go/internal/namedservice"
)

// ConnectionName selects an instance, distinct from names of other service families.
type ConnectionName string

// Validate checks that the connection name is a valid named-service identifier.
func (n ConnectionName) Validate() error { return namedservice.Validate(string(n)) }

// NamedConnection borrows one existing instance. Selection never creates another owner.
type NamedConnection struct {
	Name  ConnectionName
	Value *Connection
}

// Connections is immutable; default and named selection alias the same instance.
type Connections struct {
	registry *namedservice.Registry[ConnectionName, Connection]
}

// NewConnections validates named entries and the selected default, borrowing the existing connections.
func NewConnections(selected ConnectionName, entries ...NamedConnection) (*Connections, error) {
	values := make([]namedservice.Entry[ConnectionName, Connection], len(entries))
	for i, e := range entries {
		values[i] = namedservice.Entry[ConnectionName, Connection]{Name: e.Name, Value: e.Value}
	}
	r, err := namedservice.New(selected, values)
	if err != nil {
		return nil, err
	}
	return &Connections{registry: r}, nil
}

// Connection returns the registered connection for name; unknown names return an error.
func (r *Connections) Connection(name ConnectionName) (*Connection, error) {
	if r == nil {
		return (*namedservice.Registry[ConnectionName, Connection])(nil).Get(name)
	}
	return r.registry.Get(name)
}

// Default returns the existing connection selected as the default.
func (r *Connections) Default() (*Connection, error) {
	if r == nil {
		return (*namedservice.Registry[ConnectionName, Connection])(nil).Default()
	}
	return r.registry.Default()
}

// Names returns an owned, sorted snapshot of registered connection names.
func (r *Connections) Names() []ConnectionName {
	if r == nil {
		return nil
	}
	return r.registry.Names()
}

// DefaultName returns the selected default name, or an empty name for a nil registry.
func (r *Connections) DefaultName() ConnectionName {
	if r == nil {
		return ""
	}
	return r.registry.DefaultName()
}
