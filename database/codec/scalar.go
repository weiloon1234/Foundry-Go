package codec

import (
	"database/sql/driver"
	"math"
	"strconv"
	"strings"
	"unicode/utf8"
)

type signed interface {
	~int | ~int8 | ~int16 | ~int32 | ~int64
}
type unsigned interface {
	~uint | ~uint8 | ~uint16 | ~uint32 | ~uint64
}
type floating interface{ ~float32 | ~float64 }

func text(source any) (string, error) {
	switch source := source.(type) {
	case string:
		return source, nil
	case []byte:
		return string(source), nil
	default:
		return "", invalid()
	}
}

func validString(v string) bool { return utf8.ValidString(v) && !strings.ContainsRune(v, 0) }

// String binds text without implicit conversions from numbers or booleans.
// Invalid UTF-8 and NUL fail before PostgreSQL execution.
func String[T ~string]() Codec[T] {
	return typed(TypeText, func(v T) (driver.Value, error) {
		if !validString(string(v)) {
			return nil, invalid()
		}
		return string(v), nil
	}, func(source any) (T, error) {
		v, err := text(source)
		if err != nil || !validString(v) {
			return *new(T), invalid()
		}
		return T(v), nil
	})
}

// Bool accepts native driver booleans, with no truthiness or numeric conversion.
func Bool[T ~bool]() Codec[T] {
	return typed(TypeBoolean, func(v T) (driver.Value, error) { return bool(v), nil }, func(source any) (T, error) {
		v, ok := source.(bool)
		if !ok {
			return *new(T), invalid()
		}
		return T(v), nil
	})
}

func integer(source any) (int64, error) {
	if v, ok := source.(int64); ok {
		return v, nil
	}
	v, err := text(source)
	if err != nil {
		return 0, invalid()
	}
	parsed, err := strconv.ParseInt(v, 10, 64)
	if err != nil {
		return 0, invalid()
	}
	return parsed, nil
}

// Signed checks destination width on scan. Fractional/float sources are rejected.
func Signed[T signed]() Codec[T] {
	return typed(TypeInteger, func(v T) (driver.Value, error) { return int64(v), nil }, func(source any) (T, error) {
		v, err := integer(source)
		if err != nil || int64(T(v)) != v {
			return *new(T), invalid()
		}
		return T(v), nil
	})
}

// Unsigned requires values to fit PostgreSQL's signed bigint range as well as
// the concrete Go destination. Use Decimal for larger exact nonnegative values.
func Unsigned[T unsigned]() Codec[T] {
	return typed(TypeInteger, func(v T) (driver.Value, error) {
		if uint64(v) > math.MaxInt64 {
			return nil, invalid()
		}
		return int64(v), nil
	}, func(source any) (T, error) {
		v, err := integer(source)
		if err != nil || v < 0 || uint64(T(v)) != uint64(v) {
			return *new(T), invalid()
		}
		return T(v), nil
	})
}

// Float preserves the chosen approximate IEEE type. Non-finite values and
// narrowing overflow/underflow fail; ordinary float32 rounding is permitted.
func Float[T floating]() Codec[T] {
	return typed(TypeFloat, func(v T) (driver.Value, error) {
		if math.IsNaN(float64(v)) || math.IsInf(float64(v), 0) {
			return nil, invalid()
		}
		return float64(v), nil
	}, func(source any) (T, error) {
		v, ok := source.(float64)
		if !ok {
			return *new(T), invalid()
		}
		converted := T(v)
		if math.IsNaN(v) || math.IsInf(v, 0) || math.IsInf(float64(converted), 0) || (v != 0 && converted == 0) {
			return *new(T), invalid()
		}
		return converted, nil
	})
}
