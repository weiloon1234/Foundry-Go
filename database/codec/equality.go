package codec

import "github.com/weiloon1234/Foundry-Go/value"

// WithEquality compares decoded values directly for model change detection,
// instead of comparing their bound driver values. Use it only for a type whose
// binding is not deterministic, such as a randomized encryption envelope; the
// function must be safe for concurrent use and must not retain its inputs.
func (c Codec[T]) WithEquality(equal func(T, T) bool) Codec[T] { return c.withEquality(equal) }

func (c Codec[T]) withEquality(equal func(T, T) bool) Codec[T] {
	c.equal = equal
	return c
}

// Equal reports whether two values are the same stored value under the codec's
// WithEquality comparison. defined is false when the codec compares bound values.
func (c Codec[T]) Equal(left, right T) (same, defined bool) {
	if c.equal == nil {
		return false, false
	}
	return c.equal(left, right), true
}

// ComparesValues reports whether the codec defines WithEquality.
func (c Codec[T]) ComparesValues() bool { return c.equal != nil }

func nullableEquality[T any](equal func(T, T) bool) func(value.Nullable[T], value.Nullable[T]) bool {
	if equal == nil {
		return nil
	}
	return func(left, right value.Nullable[T]) bool {
		a, aPresent := left.Get()
		b, bPresent := right.Get()
		if !aPresent || !bPresent {
			return aPresent == bPresent
		}
		return equal(a, b)
	}
}
