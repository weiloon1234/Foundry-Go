package collection

// Number is the set of built-in numeric types accepted by Sum and Average.
// Exact decimal and money totals belong to the decimal packages.
type Number interface {
	~int | ~int8 | ~int16 | ~int32 | ~int64 |
		~uint | ~uint8 | ~uint16 | ~uint32 | ~uint64 | ~uintptr |
		~float32 | ~float64
}

// Sum adds values with ordinary Go arithmetic, including integer wraparound.
func Sum[S ~[]T, T Number](input S) T {
	var total T
	for _, value := range input {
		total += value
	}
	return total
}

// SumBy adds one selected number per item with ordinary Go arithmetic.
func SumBy[S ~[]T, T any, N Number](input S, value func(T) N) N {
	var total N
	for _, item := range input {
		total += value(item)
	}
	return total
}

// Average returns the float64 arithmetic mean, or false for no values. The
// running total is float64, so integers beyond 2^53 lose precision; use
// decimal.Sum and Div for exact results.
func Average[S ~[]T, T Number](input S) (float64, bool) {
	if len(input) == 0 {
		return 0, false
	}
	total := 0.0
	for _, value := range input {
		total += float64(value)
	}
	return total / float64(len(input)), true
}

// AverageBy is Average over one selected number per item.
func AverageBy[S ~[]T, T any, N Number](input S, value func(T) N) (float64, bool) {
	if len(input) == 0 {
		return 0, false
	}
	total := 0.0
	for _, item := range input {
		total += float64(value(item))
	}
	return total / float64(len(input)), true
}

// CountBy counts items per key. Map iteration has normal unspecified order.
func CountBy[S ~[]T, T any, K comparable](input S, key func(T) K) map[K]int {
	result := make(map[K]int)
	for _, value := range input {
		result[key(value)]++
	}
	return result
}
