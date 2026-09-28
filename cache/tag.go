package cache

import "context"

// TagDeclaration owns a tag family's concrete key type. It shares declaration
// ownership and limits with value/counter families within the same Store.
type TagDeclaration[K any] struct{ declaration Declaration[K, TagVersion] }

func DefineTag[K any](name Name, keys KeyCodec[K]) TagDeclaration[K] {
	codec := NewCodec(func(v TagVersion) ([]byte, error) {
		if err := v.Validate(); err != nil {
			return nil, err
		}
		return v.Bytes(), nil
	}, ParseTagVersion)
	return TagDeclaration[K]{declaration: Define(name, keys, codec)}
}
func (d TagDeclaration[K]) Name() Name      { return d.declaration.Name() }
func (d TagDeclaration[K]) Validate() error { return d.declaration.Validate() }
func (d TagDeclaration[K]) Bind(store *Store) (Tags[K], error) {
	if err := d.Validate(); err != nil {
		return Tags[K]{}, err
	}
	if _, err := tagCapability(store); err != nil {
		return Tags[K]{}, err
	}
	bound, err := d.declaration.Bind(store)
	if err != nil {
		return Tags[K]{}, err
	}
	return Tags[K]{cache: bound}, nil
}

// Tags retains the declared key type when creating references or invalidating.
type Tags[K any] struct{ cache Cache[K, TagVersion] }

// Tag is an opaque bound reference. The key is encoded during an operation, under
// its context and error boundary. Keep reference-backed key values immutable.
type Tag struct {
	store   *Store
	address func() (EntryKey, error)
}

func (t Tags[K]) For(key K) Tag {
	return Tag{store: t.cache.store, address: func() (EntryKey, error) { return t.cache.address(key) }}
}
func (t Tags[K]) Invalidate(ctx context.Context, key K) error {
	return t.cache.store.InvalidateTags(ctx, t.For(key))
}
