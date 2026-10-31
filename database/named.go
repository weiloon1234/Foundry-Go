package database

import "github.com/weiloon1234/Foundry-Go/internal/namedservice"

// ConnectionName identifies one configured database instance, distinct from other service families.
type ConnectionName string

// Validate returns fault.Invalid unless the connection name is a semantic identifier.
func (n ConnectionName) Validate() error { return namedservice.Validate(string(n)) }

// Connection binds a typed name to an existing borrowed instance.
type Connection struct {
	Name  ConnectionName
	Value *DB
}

// Connections is immutable. Default and named access alias the same owned instance.
// Resolve dependencies during construction; pass concrete services to domain code.
type Connections struct {
	registry *namedservice.Registry[ConnectionName, DB]
}

// NewConnections validates and snapshots named borrowed connections with an explicit default.
// It creates no additional database resources.
func NewConnections(selected ConnectionName, entries ...Connection) (*Connections, error) {
	values := make([]namedservice.Entry[ConnectionName, DB], len(entries))
	for i, e := range entries {
		values[i] = namedservice.Entry[ConnectionName, DB]{Name: e.Name, Value: e.Value}
	}
	registry, err := namedservice.New(selected, values)
	if err != nil {
		return nil, err
	}
	return &Connections{registry: registry}, nil
}

// Connection returns the borrowed database registered under name.
// An unknown name returns fault.Missing rather than selecting the default.
func (r *Connections) Connection(name ConnectionName) (*DB, error) {
	if r == nil {
		return (*namedservice.Registry[ConnectionName, DB])(nil).Get(name)
	}
	return r.registry.Get(name)
}

// Default returns the borrowed database selected at construction.
func (r *Connections) Default() (*DB, error) {
	if r == nil {
		return (*namedservice.Registry[ConnectionName, DB])(nil).Default()
	}
	return r.registry.Default()
}

// Names returns an owned, sorted list of registered names, or nil for a nil receiver.
func (r *Connections) Names() []ConnectionName {
	if r == nil {
		return nil
	}
	return r.registry.Names()
}

// DefaultName returns the selected name, or an empty name for a nil receiver.
func (r *Connections) DefaultName() ConnectionName {
	if r == nil {
		return ""
	}
	return r.registry.DefaultName()
}
