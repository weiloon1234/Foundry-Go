package cache

import (
	"github.com/weiloon1234/Foundry-Go/fault"
	"github.com/weiloon1234/Foundry-Go/internal/cacheint"
)

// CounterDeclaration binds one concrete key type to exact signed int64 values.
// Copies reuse the underlying declaration identity. The zero value is invalid.
type CounterDeclaration[K any] struct{ declaration Declaration[K, int64] }

func DefineCounter[K any](name Name, keys KeyCodec[K]) CounterDeclaration[K] {
	codec := NewCodec(func(value int64) ([]byte, error) { return cacheint.Encode(value), nil }, cacheint.Decode)
	return CounterDeclaration[K]{declaration: Define(name, keys, codec)}
}
func (d CounterDeclaration[K]) Name() Name      { return d.declaration.Name() }
func (d CounterDeclaration[K]) Validate() error { return d.declaration.Validate() }

// Bind validates the counter capability and full int64 byte bound before claiming
// a family. Value caches and counters share the Store's declaration ownership and
// capacity checks. Construction performs no adapter operation.
func (d CounterDeclaration[K]) Bind(store *Store) (Counter[K], error) {
	if err := d.Validate(); err != nil {
		return Counter[K]{}, err
	}
	if store == nil || store.backend == nil {
		return Counter[K]{}, fault.New(fault.Invalid, "cache counter requires an initialized store")
	}
	backend, ok := store.backend.(CounterBackend)
	if !ok {
		return Counter[K]{}, fault.New(fault.Invalid, "cache backend does not support atomic counters")
	}
	if _, scoped := store.backend.(TaggedBackend); scoped {
		if _, ok := store.backend.(TaggedCounterBackend); !ok {
			return Counter[K]{}, fault.New(fault.Invalid, "cache backend requires tagged counters for namespace protection")
		}
	}
	if store.config.MaxValueBytes < cacheint.MaxBytes {
		return Counter[K]{}, fault.New(fault.Invalid, "cache counter requires space for a full int64 value")
	}
	bound, err := d.declaration.Bind(store)
	if err != nil {
		return Counter[K]{}, err
	}
	return Counter[K]{cache: bound, backend: backend}, nil
}
