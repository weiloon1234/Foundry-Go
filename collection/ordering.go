package collection

import (
	"cmp"
	"slices"
)

// Chunk splits input into consecutive chunks of at most size items. Chunks
// share one new backing array, independent of input, and each chunk's capacity
// equals its length, so appending to one chunk never overwrites the next.
// Nil input returns nil. A size below one panics, like slices.Chunk.
func Chunk[S ~[]T, T any](input S, size int) []S {
	if size < 1 {
		panic("collection: Chunk size must be positive")
	}
	if input == nil {
		return nil
	}
	owned := slices.Clone(input)
	result := make([]S, 0, (len(owned)+size-1)/size)
	for start := 0; start < len(owned); start += size {
		end := min(start+size, len(owned))
		result = append(result, owned[start:end:end])
	}
	return result
}

// Unique retains the first occurrence of each value, in input order.
func Unique[S ~[]T, T comparable](input S) S {
	if input == nil {
		return nil
	}
	seen := make(map[T]struct{}, len(input))
	result := make(S, 0, len(input))
	for _, value := range input {
		if _, ok := seen[value]; !ok {
			seen[value] = struct{}{}
			result = append(result, value)
		}
	}
	return result
}

type keyed[T any, K cmp.Ordered] struct {
	key   K
	value T
}

// SortBy returns a new slice ordered by ascending key. The sort is stable, and
// key runs exactly once per item. Floating-point NaN keys sort first, following
// cmp.Compare.
func SortBy[S ~[]T, T any, K cmp.Ordered](input S, key func(T) K) S {
	return sortBy(input, key, cmp.Compare[K])
}

// SortByDesc is SortBy with descending keys; equal keys keep input order.
func SortByDesc[S ~[]T, T any, K cmp.Ordered](input S, key func(T) K) S {
	return sortBy(input, key, func(a, b K) int { return cmp.Compare(b, a) })
}

func sortBy[S ~[]T, T any, K cmp.Ordered](input S, key func(T) K, compare func(K, K) int) S {
	if input == nil {
		return nil
	}
	items := make([]keyed[T, K], len(input))
	for i, value := range input {
		items[i] = keyed[T, K]{key(value), value}
	}
	slices.SortStableFunc(items, func(a, b keyed[T, K]) int { return compare(a.key, b.key) })
	result := make(S, len(items))
	for i, item := range items {
		result[i] = item.value
	}
	return result
}

// MinBy returns the first item with the smallest key, or false for no items.
func MinBy[S ~[]T, T any, K cmp.Ordered](input S, key func(T) K) (T, bool) {
	return selectBy(input, key, -1)
}

// MaxBy returns the first item with the largest key, or false for no items.
func MaxBy[S ~[]T, T any, K cmp.Ordered](input S, key func(T) K) (T, bool) {
	return selectBy(input, key, 1)
}

func selectBy[S ~[]T, T any, K cmp.Ordered](input S, key func(T) K, want int) (T, bool) {
	if len(input) == 0 {
		return *new(T), false
	}
	best, bestKey := input[0], key(input[0])
	for _, value := range input[1:] {
		if k := key(value); cmp.Compare(k, bestKey) == want {
			best, bestKey = value, k
		}
	}
	return best, true
}
