// Package collection provides focused transformations over ordinary Go slices.
// Inputs remain unchanged; callbacks run synchronously. Result containers are
// independent, while pointer/map/slice elements retain ordinary Go aliasing.
// Use slices/maps/iter for sorting, reversing, cloning and iteration.
package collection

func Map[S ~[]T, T, U any](input S, transform func(T) U) []U {
	if input == nil {
		return nil
	}
	result := make([]U, len(input))
	for i, value := range input {
		result[i] = transform(value)
	}
	return result
}

func Filter[S ~[]T, T any](input S, keep func(T) bool) S {
	if input == nil {
		return nil
	}
	result := make(S, 0, len(input))
	for _, value := range input {
		if keep(value) {
			result = append(result, value)
		}
	}
	return result
}
func FlatMap[S ~[]T, T, U any](input S, transform func(T) []U) []U {
	if input == nil {
		return nil
	}
	result := make([]U, 0)
	for _, value := range input {
		result = append(result, transform(value)...)
	}
	return result
}

// KeyBy keeps the last item for each key, matching ordinary map assignment.
func KeyBy[S ~[]T, T any, K comparable](input S, key func(T) K) map[K]T {
	result := make(map[K]T, len(input))
	for _, value := range input {
		result[key(value)] = value
	}
	return result
}

// GroupBy preserves input order inside every group. Map iteration is unordered.
func GroupBy[S ~[]T, T any, K comparable](input S, key func(T) K) map[K][]T {
	result := make(map[K][]T)
	for _, value := range input {
		k := key(value)
		result[k] = append(result[k], value)
	}
	return result
}

// UniqueBy retains the first item for each key, in input order.
func UniqueBy[S ~[]T, T any, K comparable](input S, key func(T) K) S {
	if input == nil {
		return nil
	}
	seen := make(map[K]bool, len(input))
	result := make(S, 0, len(input))
	for _, value := range input {
		k := key(value)
		if !seen[k] {
			seen[k] = true
			result = append(result, value)
		}
	}
	return result
}
func Partition[S ~[]T, T any](input S, keep func(T) bool) (S, S) {
	if input == nil {
		return nil, nil
	}
	yes, no := make(S, 0), make(S, 0)
	for _, value := range input {
		if keep(value) {
			yes = append(yes, value)
		} else {
			no = append(no, value)
		}
	}
	return yes, no
}
func Reduce[S ~[]T, T, U any](input S, initial U, combine func(U, T) U) U {
	result := initial
	for _, value := range input {
		result = combine(result, value)
	}
	return result
}
func Find[S ~[]T, T any](input S, match func(T) bool) (T, bool) {
	for _, value := range input {
		if match(value) {
			return value, true
		}
	}
	return *new(T), false
}
func Any[S ~[]T, T any](input S, match func(T) bool) bool { _, ok := Find(input, match); return ok }
func All[S ~[]T, T any](input S, match func(T) bool) bool {
	for _, value := range input {
		if !match(value) {
			return false
		}
	}
	return true
}
func Count[S ~[]T, T any](input S, match func(T) bool) int {
	count := 0
	for _, value := range input {
		if match(value) {
			count++
		}
	}
	return count
}
