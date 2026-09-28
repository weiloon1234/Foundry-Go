package data

import (
	"context"

	"github.com/weiloon1234/Foundry-Go/keyspace"
)

// Hash is a typed collection of fields at each resource key. Point writes preserve
// expiry and return whether a field was newly inserted. Values are canonical JSON
// snapshots; use DTOs that explicitly select model getters for presentation data.
type Hash[K, F, V any] struct {
	handle[K]
	fields  keyspace.Codec[F]
	backend HashBackend
}

func (h Hash[K, F, V]) field(ctx context.Context, field F, l Limits) (string, error) {
	text, err := h.fields.Encode(field)
	if err != nil {
		return "", err
	}
	if err := ctx.Err(); err != nil {
		return "", err
	}
	return text, l.ValidateField(text)
}
func (h Hash[K, F, V]) Get(ctx context.Context, key K, field F) (V, bool, error) {
	var result V
	var found bool
	err := h.execute(ctx, key, func(ctx context.Context, k Key, l Limits) error {
		name, err := h.field(ctx, field, l)
		if err != nil {
			return err
		}
		text, hit, err := h.backend.HashGet(ctx, k, name, l)
		if err != nil {
			return err
		}
		found = hit
		if !hit {
			return nil
		}
		if err := ctx.Err(); err != nil {
			return err
		}
		result, err = decode[V](text, l)
		return err
	})
	if err != nil {
		return *new(V), false, err
	}
	return result, found, nil
}
func (h Hash[K, F, V]) Set(ctx context.Context, key K, field F, input V) (bool, error) {
	var added bool
	err := h.execute(ctx, key, func(ctx context.Context, k Key, l Limits) error {
		name, err := h.field(ctx, field, l)
		if err != nil {
			return err
		}
		text, err := encode(input, l)
		if err != nil {
			return err
		}
		if err := ctx.Err(); err != nil {
			return err
		}
		added, err = h.backend.HashSet(ctx, k, name, text, l)
		return err
	})
	if err != nil {
		return false, err
	}
	return added, nil
}
func (h Hash[K, F, V]) DeleteField(ctx context.Context, key K, field F) (bool, error) {
	var removed bool
	err := h.execute(ctx, key, func(ctx context.Context, k Key, l Limits) error {
		name, err := h.field(ctx, field, l)
		if err != nil {
			return err
		}
		removed, err = h.backend.HashDelete(ctx, k, name, l)
		return err
	})
	if err != nil {
		return false, err
	}
	return removed, nil
}
