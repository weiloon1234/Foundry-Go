package redis

import "github.com/weiloon1234/Foundry-Go/internal/namedservice"

// ConnectionName identifies one configured redis instance, distinct from other service families.
type ConnectionName string

// Validate checks the configured service name.
func (n ConnectionName) Validate() error { return namedservice.Validate(string(n)) }

// Connection binds a typed name to an existing borrowed instance.
type Connection struct {
	Name  ConnectionName
	Value *Client
}

// Connections is immutable. Default and named access alias the same owned instance.
// Resolve dependencies during construction; pass concrete services to domain code.
type Connections struct {
	registry *namedservice.Registry[ConnectionName, Client]
}

// NewConnections validates named clients and the selected default while borrowing their lifecycles.
func NewConnections(selected ConnectionName, entries ...Connection) (*Connections, error) {
	values := make([]namedservice.Entry[ConnectionName, Client], len(entries))
	for i, e := range entries {
		values[i] = namedservice.Entry[ConnectionName, Client]{Name: e.Name, Value: e.Value}
	}
	registry, err := namedservice.New(selected, values)
	if err != nil {
		return nil, err
	}
	return &Connections{registry: registry}, nil
}

// Connection returns the registered client or an error if name is unknown.
func (r *Connections) Connection(name ConnectionName) (*Client, error) {
	if r == nil {
		return (*namedservice.Registry[ConnectionName, Client])(nil).Get(name)
	}
	return r.registry.Get(name)
}

// Default returns the selected registered client or an error for an uninitialized registry.
func (r *Connections) Default() (*Client, error) {
	if r == nil {
		return (*namedservice.Registry[ConnectionName, Client])(nil).Default()
	}
	return r.registry.Default()
}

// Names returns an owned, sorted list of names, or nil for a nil registry.
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
