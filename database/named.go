package database

import "github.com/weiloon1234/Foundry-Go/internal/namedservice"

// ConnectionName identifies one configured database instance, distinct from other service families.
type ConnectionName string

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
func (r *Connections) Connection(name ConnectionName) (*DB, error) {
	if r == nil {
		return (*namedservice.Registry[ConnectionName, DB])(nil).Get(name)
	}
	return r.registry.Get(name)
}
func (r *Connections) Default() (*DB, error) {
	if r == nil {
		return (*namedservice.Registry[ConnectionName, DB])(nil).Default()
	}
	return r.registry.Default()
}
func (r *Connections) Names() []ConnectionName {
	if r == nil {
		return nil
	}
	return r.registry.Names()
}
func (r *Connections) DefaultName() ConnectionName {
	if r == nil {
		return ""
	}
	return r.registry.DefaultName()
}
