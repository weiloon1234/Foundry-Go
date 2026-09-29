package data

import (
	"context"

	"github.com/weiloon1234/Foundry-Go/fault"
)

// List preserves resource/element types in insertion order. Its length is
// bounded by Limits.Entries: a push that would exceed it inserts nothing.
// New lists persist; writes retain TTL. Removing the last element removes it.
type List[K, V any] struct {
	handle[K]
	backend ListBackend
}

// Push appends values at the tail in order and returns the new length.
func (l List[K, V]) Push(ctx context.Context, key K, values ...V) (uint64, error) {
	return l.push(ctx, key, Back, values)
}

// PushFront inserts values at the head one at a time, like Redis LPUSH, so the
// last argument becomes the first element. It returns the new length.
func (l List[K, V]) PushFront(ctx context.Context, key K, values ...V) (uint64, error) {
	return l.push(ctx, key, Front, values)
}
func (l List[K, V]) push(ctx context.Context, key K, end End, values []V) (uint64, error) {
	var length uint64
	err := l.execute(ctx, key, func(ctx context.Context, k Key, limits Limits) error {
		if len(values) == 0 || len(values) > limits.Entries {
			return fault.New(fault.Invalid, "Redis list push exceeds its bound")
		}
		texts := make([]string, len(values))
		for i, input := range values {
			var err error
			if texts[i], err = encode(input, limits); err != nil {
				return err
			}
			if err := ctx.Err(); err != nil {
				return err
			}
		}
		var err error
		length, err = l.backend.ListPush(ctx, k, texts, end, limits)
		if err == nil && (length < uint64(len(texts)) || length > uint64(limits.Entries)) {
			return fault.New(fault.Internal, "invalid Redis list length")
		}
		return err
	})
	if err != nil {
		return 0, err
	}
	return length, nil
}

// PopFront removes and returns the head element, or false for an empty list.
func (l List[K, V]) PopFront(ctx context.Context, key K) (V, bool, error) {
	return l.pop(ctx, key, Front)
}

// PopBack removes and returns the tail element, or false for an empty list.
func (l List[K, V]) PopBack(ctx context.Context, key K) (V, bool, error) {
	return l.pop(ctx, key, Back)
}

// pop checks the element's size before removal. A removed element that fails
// JSON decoding is reported as an error; the removal is not undone.
func (l List[K, V]) pop(ctx context.Context, key K, end End) (V, bool, error) {
	var result V
	var found bool
	err := l.execute(ctx, key, func(ctx context.Context, k Key, limits Limits) error {
		text, hit, err := l.backend.ListPop(ctx, k, end, limits)
		if err != nil || !hit {
			return err
		}
		result, err = decode[V](text, limits)
		found = err == nil
		return err
	})
	if err != nil {
		return *new(V), false, err
	}
	return result, found, nil
}

// Range returns elements from start through stop inclusive, using Redis index
// semantics (negative indices count from the tail). The reply is bounded by
// Limits.Entries and Limits.ReplyBytes; a missing list returns an empty slice.
func (l List[K, V]) Range(ctx context.Context, key K, start, stop int64) ([]V, error) {
	if err := ValidateIndexRange(start, stop); err != nil {
		return nil, err
	}
	var result []V
	err := l.execute(ctx, key, func(ctx context.Context, k Key, limits Limits) error {
		texts, err := l.backend.ListRange(ctx, k, start, stop, limits)
		if err != nil {
			return err
		}
		if len(texts) > limits.Entries {
			return fault.New(fault.Invalid, "Redis list exceeds its cardinality bound")
		}
		result = make([]V, 0, len(texts))
		remaining := limits.ReplyBytes
		for _, text := range texts {
			if err := ctx.Err(); err != nil {
				return err
			}
			if len(text) > remaining {
				return fault.New(fault.Invalid, "Redis list reply exceeds its bound")
			}
			remaining -= len(text)
			item, err := decode[V](text, limits)
			if err != nil {
				return err
			}
			result = append(result, item)
		}
		return nil
	})
	if err != nil {
		return nil, err
	}
	return result, nil
}

// Trim keeps only elements from start through stop inclusive (Redis LTRIM).
func (l List[K, V]) Trim(ctx context.Context, key K, start, stop int64) error {
	if err := ValidateIndexRange(start, stop); err != nil {
		return err
	}
	return l.execute(ctx, key, func(ctx context.Context, k Key, limits Limits) error {
		return l.backend.ListTrim(ctx, k, start, stop, limits)
	})
}
