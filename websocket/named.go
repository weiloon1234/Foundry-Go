package websocket

import (
	"github.com/weiloon1234/Foundry-Go/internal/namedservice"
)

// ConnectionName selects an instance, distinct from names of other service families.
type ConnectionName string

// Validate checks the connection's service name.
func (n ConnectionName) Validate() error { return namedservice.Validate(string(n)) }

// NamedConnection borrows one existing instance. Selection never creates another owner.
type NamedConnection struct {
	Name  ConnectionName
	Value *BackendConnection
}

// Connections is immutable; default and named selection alias the same instance.
type Connections struct {
	registry *namedservice.Registry[ConnectionName, BackendConnection]
}

// NewConnections creates a named connection registry with the selected connection as
// its default.
func NewConnections(selected ConnectionName, entries ...NamedConnection) (*Connections, error) {
	values := make([]namedservice.Entry[ConnectionName, BackendConnection], len(entries))
	for i, e := range entries {
		values[i] = namedservice.Entry[ConnectionName, BackendConnection]{Name: e.Name, Value: e.Value}
	}
	r, err := namedservice.New(selected, values)
	if err != nil {
		return nil, err
	}
	return &Connections{registry: r}, nil
}

// Connection resolves an explicit name or returns an error if it is unavailable.
func (r *Connections) Connection(name ConnectionName) (*BackendConnection, error) {
	if r == nil {
		return (*namedservice.Registry[ConnectionName, BackendConnection])(nil).Get(name)
	}
	return r.registry.Get(name)
}

// Default returns the selected connection or an error if no default is available.
func (r *Connections) Default() (*BackendConnection, error) {
	if r == nil {
		return (*namedservice.Registry[ConnectionName, BackendConnection])(nil).Default()
	}
	return r.registry.Default()
}

// Names returns an owned snapshot of registered connection names.
func (r *Connections) Names() []ConnectionName {
	if r == nil {
		return nil
	}
	return r.registry.Names()
}

// DefaultName returns the selected name, or an empty name for a nil registry.
func (r *Connections) DefaultName() ConnectionName {
	if r == nil {
		return ""
	}
	return r.registry.DefaultName()
}
