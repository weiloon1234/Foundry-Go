package data

import (
	"context"

	"github.com/weiloon1234/Foundry-Go/fault"
	"github.com/weiloon1234/Foundry-Go/keyspace"
	"github.com/weiloon1234/Foundry-Go/value"
)

// Hash is a typed collection of fields at each resource key. Point writes preserve
// expiry and return whether a field was newly inserted. Values are canonical JSON
// snapshots; use DTOs that explicitly select model getters for presentation data.
type Hash[K, F, V any] struct {
	handle[K]
	fields  keyspace.Codec[F]
	parse   func(string) (F, error)
	backend HashBackend
}

// HashEntry is one typed field and value read by GetAll.
type HashEntry[F, V any] struct {
	Field F
	Value V
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

// GetMany reads several fields of one resource in one atomic operation. The
// result has one entry per requested field, in input order; missing fields are
// unset. Repeated fields repeat their value. The field count is bounded by
// Limits.Entries and the returned payload by Limits.ReplyBytes.
func (h Hash[K, F, V]) GetMany(ctx context.Context, key K, fields ...F) ([]value.Optional[V], error) {
	var result []value.Optional[V]
	err := h.execute(ctx, key, func(ctx context.Context, k Key, l Limits) error {
		backend, ok := h.backend.(HashReadBackend)
		if !ok {
			return fault.New(fault.Invalid, "adapter does not support Redis hash batch reads")
		}
		if len(fields) == 0 || len(fields) > l.Entries {
			return fault.New(fault.Invalid, "Redis hash field batch exceeds its bound")
		}
		names := make([]string, len(fields))
		for i, field := range fields {
			var err error
			if names[i], err = h.field(ctx, field, l); err != nil {
				return err
			}
		}
		texts, err := backend.HashGetMany(ctx, k, names, l)
		if err != nil {
			return err
		}
		if len(texts) != len(names) {
			return fault.New(fault.Internal, "invalid Redis hash batch reply")
		}
		result = make([]value.Optional[V], len(texts))
		remaining := l.ReplyBytes
		for i, stored := range texts {
			text, found := stored.Get()
			if !found {
				continue
			}
			if err := ctx.Err(); err != nil {
				return err
			}
			if len(text) > remaining {
				return fault.New(fault.Invalid, "Redis hash reply exceeds its bound")
			}
			remaining -= len(text)
			item, err := decode[V](text, l)
			if err != nil {
				return err
			}
			result[i] = value.Set(item)
		}
		return nil
	})
	if err != nil {
		return nil, err
	}
	return result, nil
}

// GetAll returns every field of one resource, ordered by encoded field bytes.
// The declaration must supply WithFieldDecoder; each decoded field must encode
// back to its stored bytes. Cardinality and reply bounds are checked before the
// adapter transfers payloads. A missing hash returns an empty slice.
func (h Hash[K, F, V]) GetAll(ctx context.Context, key K) ([]HashEntry[F, V], error) {
	var result []HashEntry[F, V]
	err := h.execute(ctx, key, func(ctx context.Context, k Key, l Limits) error {
		backend, ok := h.backend.(HashReadBackend)
		if !ok {
			return fault.New(fault.Invalid, "adapter does not support Redis hash batch reads")
		}
		if h.parse == nil {
			return fault.New(fault.Invalid, "Redis hash declaration has no field decoder")
		}
		stored, err := backend.HashGetAll(ctx, k, l)
		if err != nil {
			return err
		}
		if len(stored) > l.Entries {
			return fault.New(fault.Invalid, "Redis hash exceeds its cardinality bound")
		}
		result = make([]HashEntry[F, V], 0, len(stored))
		remaining := l.ReplyBytes
		for i, entry := range stored {
			if err := ctx.Err(); err != nil {
				return err
			}
			if l.ValidateField(entry.Field) != nil || len(entry.Value) > remaining || i > 0 && entry.Field <= stored[i-1].Field {
				return fault.New(fault.Invalid, "Redis hash reply exceeds its bound or is not canonical")
			}
			remaining -= len(entry.Value)
			field, err := h.parse(entry.Field)
			if err != nil {
				return err
			}
			if encoded, err := h.fields.Encode(field); err != nil || encoded != entry.Field {
				return fault.New(fault.Invalid, "Redis hash field decoder does not invert its codec")
			}
			item, err := decode[V](entry.Value, l)
			if err != nil {
				return err
			}
			result = append(result, HashEntry[F, V]{Field: field, Value: item})
		}
		return nil
	})
	if err != nil {
		return nil, err
	}
	return result, nil
}

// IncrementField atomically adds a signed delta to an int64-valued hash field
// and returns the result. A missing field starts at zero; existing expiry is
// retained. A stored value that is not a canonical integer, or overflow, fails
// without mutation. It is a function because only int64 hashes support it.
func IncrementField[K, F any](ctx context.Context, h Hash[K, F, int64], key K, field F, delta int64) (int64, error) {
	var result int64
	err := h.execute(ctx, key, func(ctx context.Context, k Key, l Limits) error {
		backend, ok := h.backend.(HashCounterBackend)
		if !ok {
			return fault.New(fault.Invalid, "adapter does not support Redis hash counters")
		}
		name, err := h.field(ctx, field, l)
		if err != nil {
			return err
		}
		result, err = backend.HashIncrement(ctx, k, name, delta, l)
		return err
	})
	if err != nil {
		return 0, err
	}
	return result, nil
}
