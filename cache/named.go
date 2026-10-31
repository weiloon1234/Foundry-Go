package cache

import "github.com/weiloon1234/Foundry-Go/internal/namedservice"

// StoreName identifies one configured cache instance, distinct from other service families.
type StoreName string

// Validate checks the configured service name.
func (n StoreName) Validate() error { return namedservice.Validate(string(n)) }

// NamedStore binds a typed name to an existing borrowed instance.
type NamedStore struct {
	Name  StoreName
	Value *Store
}

// Stores is immutable. Default and named access alias the same owned instance.
// Resolve dependencies during construction; pass concrete services to domain code.
type Stores struct {
	registry *namedservice.Registry[StoreName, Store]
}

// NewStores validates the named stores and selected default without taking ownership of their lifecycles.
func NewStores(selected StoreName, entries ...NamedStore) (*Stores, error) {
	values := make([]namedservice.Entry[StoreName, Store], len(entries))
	for i, e := range entries {
		values[i] = namedservice.Entry[StoreName, Store]{Name: e.Name, Value: e.Value}
	}
	registry, err := namedservice.New(selected, values)
	if err != nil {
		return nil, err
	}
	return &Stores{registry: registry}, nil
}

// Store returns the store registered under name or an error if it is unknown.
func (r *Stores) Store(name StoreName) (*Store, error) {
	if r == nil {
		return (*namedservice.Registry[StoreName, Store])(nil).Get(name)
	}
	return r.registry.Get(name)
}

// Default returns the selected registered store or an error if no default is available.
func (r *Stores) Default() (*Store, error) {
	if r == nil {
		return (*namedservice.Registry[StoreName, Store])(nil).Default()
	}
	return r.registry.Default()
}

// Names returns an owned, sorted list of registered names, or nil for a nil registry.
func (r *Stores) Names() []StoreName {
	if r == nil {
		return nil
	}
	return r.registry.Names()
}

// DefaultName returns the selected name, or an empty name for a nil registry.
func (r *Stores) DefaultName() StoreName {
	if r == nil {
		return ""
	}
	return r.registry.DefaultName()
}
