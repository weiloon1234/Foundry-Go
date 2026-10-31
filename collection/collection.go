// Package collection provides focused transformations over ordinary Go slices.
// Inputs remain unchanged; callbacks run synchronously. Result containers are
// independent, while pointer/map/slice elements retain ordinary Go aliasing.
// Use slices/maps/iter for in-place sorting, reversing, cloning and iteration;
// SortBy/SortByDesc return new slices ordered by an extracted key.
package collection

// Map transforms each element into a new slice in input order, preserving nil input.
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

// Filter returns a new slice of matching elements in input order, preserving nil input.
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

// FlatMap concatenates each transformed slice in input order, preserving nil input.
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

// Partition splits input into matching and nonmatching slices, preserving order within each. Nil
// input returns two nil slices.
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

// Reduce combines elements in input order, starting with the supplied initial value.
func Reduce[S ~[]T, T, U any](input S, initial U, combine func(U, T) U) U {
	result := initial
	for _, value := range input {
		result = combine(result, value)
	}
	return result
}

// Find returns the first matching element, or the zero value and false.
func Find[S ~[]T, T any](input S, match func(T) bool) (T, bool) {
	for _, value := range input {
		if match(value) {
			return value, true
		}
	}
	return *new(T), false
}

// Any reports whether any element matches.
func Any[S ~[]T, T any](input S, match func(T) bool) bool { _, ok := Find(input, match); return ok }

// All reports whether every element matches; empty input returns true.
func All[S ~[]T, T any](input S, match func(T) bool) bool {
	for _, value := range input {
		if !match(value) {
			return false
		}
	}
	return true
}

// Count returns the number of matching elements.
func Count[S ~[]T, T any](input S, match func(T) bool) int {
	count := 0
	for _, value := range input {
		if match(value) {
			count++
		}
	}
	return count
}
