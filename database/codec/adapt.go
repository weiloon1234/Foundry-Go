package codec

import "database/sql/driver"

// Adapt changes a codec's Go representation while preserving validation,
// ownership, equality and sensitivity. The conversions must be lossless.
func Adapt[V, U any](base Codec[U], to func(V) U, from func(U) V) Codec[V] {
	if base.Validate() != nil || to == nil || from == nil {
		return Codec[V]{}
	}
	result := New(func(v V) (driver.Value, error) { return base.encode(to(v)) }, func(raw any) (V, error) {
		u, err := base.decode(raw)
		if err != nil {
			return *new(V), err
		}
		return from(u), nil
	}).WithParameterType(base.ParameterType()).withClone(func(v V) V { return from(base.Clone(to(v))) }).withSensitivity(base.SensitiveValues())
	if base.ComparesValues() {
		result = result.WithEquality(func(a, b V) bool { equal, _ := base.Equal(to(a), to(b)); return equal })
	}
	return result
}
