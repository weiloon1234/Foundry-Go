// Package namedservice owns immutable named/default selection for service families.
package namedservice

import (
	"github.com/weiloon1234/Foundry-Go/fault"
	"github.com/weiloon1234/Foundry-Go/internal/identifier"
	"slices"
)

const MaxEntries = 128

type Name interface {
	~string
	Validate() error
}

func Validate(name string) error {
	if !identifier.Semantic(name) {
		return fault.New(fault.Invalid, "invalid service name")
	}
	return nil
}

type Entry[N Name, V any] struct {
	Name  N
	Value *V
}
type Registry[N Name, V any] struct {
	values   map[N]*V
	names    []N
	selected N
}

func New[N Name, V any](selected N, entries []Entry[N, V]) (*Registry[N, V], error) {
	if err := selected.Validate(); err != nil {
		return nil, err
	}
	if len(entries) == 0 || len(entries) > MaxEntries {
		return nil, fault.New(fault.Invalid, "named services require a bounded nonempty collection")
	}
	r := &Registry[N, V]{values: make(map[N]*V, len(entries)), selected: selected}
	for _, entry := range entries {
		if err := entry.Name.Validate(); err != nil {
			return nil, err
		}
		if entry.Value == nil {
			return nil, fault.New(fault.Invalid, "named service is nil")
		}
		if _, ok := r.values[entry.Name]; ok {
			return nil, fault.New(fault.Duplicate, "service name is duplicated")
		}
		r.values[entry.Name] = entry.Value
		r.names = append(r.names, entry.Name)
	}
	if _, ok := r.values[selected]; !ok {
		return nil, fault.New(fault.Missing, "default service is not registered")
	}
	slices.Sort(r.names)
	return r, nil
}
func (r *Registry[N, V]) Get(name N) (*V, error) {
	if r == nil {
		return nil, fault.New(fault.Invalid, "named services are not initialized")
	}
	if err := name.Validate(); err != nil {
		return nil, err
	}
	value, ok := r.values[name]
	if !ok {
		return nil, fault.New(fault.Missing, "named service is not registered")
	}
	return value, nil
}
func (r *Registry[N, V]) Default() (*V, error) {
	if r == nil {
		return nil, fault.New(fault.Invalid, "named services are not initialized")
	}
	return r.Get(r.selected)
}
func (r *Registry[N, V]) Names() []N {
	if r == nil {
		return nil
	}
	return slices.Clone(r.names)
}
func (r *Registry[N, V]) DefaultName() N {
	if r == nil {
		var zero N
		return zero
	}
	return r.selected
}
