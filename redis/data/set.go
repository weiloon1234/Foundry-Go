package data

import (
	"context"

	"github.com/weiloon1234/Foundry-Go/fault"
)

// Set preserves resource/member types and canonical JSON membership identity.
// Members returns an owned native slice ordered by encoded JSON bytes. This is
// deterministic, not numeric/domain sorting. New sets persist; writes retain TTL.
type Set[K, V any] struct {
	handle[K]
	backend SetBackend
}

func (s Set[K, V]) member(ctx context.Context, key K, input V, fn func(context.Context, Key, string, Limits) (bool, error)) (bool, error) {
	var result bool
	err := s.execute(ctx, key, func(ctx context.Context, k Key, l Limits) error {
		text, err := encode(input, l)
		if err != nil {
			return err
		}
		if err := ctx.Err(); err != nil {
			return err
		}
		result, err = fn(ctx, k, text, l)
		return err
	})
	if err != nil {
		return false, err
	}
	return result, nil
}
func (s Set[K, V]) Add(ctx context.Context, key K, input V) (bool, error) {
	return s.member(ctx, key, input, func(ctx context.Context, k Key, text string, l Limits) (bool, error) {
		return s.backend.SetAdd(ctx, k, text, l)
	})
}
func (s Set[K, V]) Remove(ctx context.Context, key K, input V) (bool, error) {
	return s.member(ctx, key, input, func(ctx context.Context, k Key, text string, l Limits) (bool, error) {
		return s.backend.SetRemove(ctx, k, text, l)
	})
}
func (s Set[K, V]) Contains(ctx context.Context, key K, input V) (bool, error) {
	return s.member(ctx, key, input, func(ctx context.Context, k Key, text string, l Limits) (bool, error) {
		return s.backend.SetContains(ctx, k, text, l)
	})
}
func (s Set[K, V]) Members(ctx context.Context, key K) ([]V, error) {
	var result []V
	err := s.execute(ctx, key, func(ctx context.Context, k Key, l Limits) error {
		texts, err := s.backend.SetMembers(ctx, k, l)
		if err != nil {
			return err
		}
		if len(texts) > l.Entries {
			return fault.New(fault.Invalid, "Redis set exceeds its cardinality bound")
		}
		result = make([]V, 0, len(texts))
		remaining := l.ReplyBytes
		for i, text := range texts {
			if err := ctx.Err(); err != nil {
				return err
			}
			if len(text) > remaining || i > 0 && text <= texts[i-1] {
				return fault.New(fault.Invalid, "Redis set reply exceeds its bound or is not canonical")
			}
			remaining -= len(text)
			item, err := decode[V](text, l)
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
