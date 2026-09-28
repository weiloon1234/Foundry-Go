package validation

import (
	"time"

	"github.com/weiloon1234/Foundry-Go/temporal"
)

// Temporal retains the distinction between instants, dates and wall times.
// A bound or related field must have the same concrete type as the input.
type Temporal interface {
	time.Time | temporal.Date | temporal.Time | temporal.DateTime | temporal.LocalDateTime
	String() string
}

// Before requires a temporal value strictly before its declared bound. Dates
// and local values compare in calendar order; instants compare in UTC without
// monotonic readings. No host timezone or current clock is consulted.
func Before[T Temporal](bound T) Rule[T] {
	return temporalBound(bound, "foundry.before", -1, false)
}

// BeforeOrEqual also accepts a value equal to the bound, retaining nanoseconds.
func BeforeOrEqual[T Temporal](bound T) Rule[T] {
	return temporalBound(bound, "foundry.before_or_equal", -1, true)
}

// After requires a value strictly after its same-type temporal bound.
func After[T Temporal](bound T) Rule[T] {
	return temporalBound(bound, "foundry.after", 1, false)
}

// AfterOrEqual accepts values at or after their same-type temporal bound.
func AfterOrEqual[T Temporal](bound T) Rule[T] {
	return temporalBound(bound, "foundry.after_or_equal", 1, true)
}

func temporalBound[T Temporal](bound T, id RuleID, direction int, equal bool) Rule[T] {
	reference, valid := temporalInstant(bound)
	if !valid {
		return failed[T](invalid("temporal validation bound is absent or out of range"))
	}
	return valueRule(Spec{ID: id, Parameters: []Parameter{parameter("type", temporalType[T]()), parameter("value", temporalText(bound))}}, false, func(_ *execution, input T) (bool, error) {
		instant, valid := temporalInstant(input)
		return valid && temporalOrder(instant.Compare(reference), direction, equal), nil
	})
}

// BeforeField compares two generated fields while preserving their request and
// temporal value types. Failures use the first field's path; metadata retains
// both field names. Absent Date/LocalDateTime values reject rather than compare
// as calendar dates. Optional and nullable states require explicit wrappers.
func BeforeField[T any, V Temporal](field, other Field[T, V]) Rule[T] {
	return Compare(field, other, temporalPair[V]("foundry.before", -1, false))
}

// BeforeOrEqualField allows equality when comparing two same-type fields.
func BeforeOrEqualField[T any, V Temporal](field, other Field[T, V]) Rule[T] {
	return Compare(field, other, temporalPair[V]("foundry.before_or_equal", -1, true))
}

// AfterField requires the first field to be strictly after the second field.
func AfterField[T any, V Temporal](field, other Field[T, V]) Rule[T] {
	return Compare(field, other, temporalPair[V]("foundry.after", 1, false))
}

// AfterOrEqualField requires the first field to be at or after the second field.
func AfterOrEqualField[T any, V Temporal](field, other Field[T, V]) Rule[T] {
	return Compare(field, other, temporalPair[V]("foundry.after_or_equal", 1, true))
}

func temporalPair[T Temporal](id RuleID, direction int, equal bool) Rule[Pair[T]] {
	return valueRule(Spec{ID: id, Parameters: []Parameter{parameter("type", temporalType[T]())}}, false, func(_ *execution, pair Pair[T]) (bool, error) {
		left, leftValid := temporalInstant(pair.Left)
		right, rightValid := temporalInstant(pair.Right)
		return leftValid && rightValid && temporalOrder(left.Compare(right), direction, equal), nil
	})
}

func temporalOrder(order, direction int, equal bool) bool {
	return order == direction || equal && order == 0
}

func temporalType[T Temporal]() string {
	switch any(*new(T)).(type) {
	case temporal.Date:
		return "date"
	case temporal.Time:
		return "time"
	case temporal.LocalDateTime:
		return "local_datetime"
	default:
		return "datetime"
	}
}

func temporalText[T Temporal](value T) string {
	switch v := any(value).(type) {
	case time.Time:
		return v.UTC().Round(0).Format(time.RFC3339Nano)
	default:
		return value.String()
	}
}

// UTC is only an ordering coordinate for wall values; this does not resolve
// them into an instant in any application timezone.
func temporalInstant[T Temporal](value T) (time.Time, bool) {
	switch v := any(value).(type) {
	case time.Time:
		instant := v.UTC().Round(0)
		return instant, instant.Year() >= 1 && instant.Year() <= 9999
	case temporal.DateTime:
		return v.UTC(), true
	case temporal.Date:
		return time.Date(v.Year(), v.Month(), v.Day(), 0, 0, 0, 0, time.UTC), !v.IsZero()
	case temporal.Time:
		return time.Date(1, time.January, 1, v.Hour(), v.Minute(), v.Second(), v.Nanosecond(), time.UTC), true
	case temporal.LocalDateTime:
		date, clock := v.Date(), v.Time()
		return time.Date(date.Year(), date.Month(), date.Day(), clock.Hour(), clock.Minute(), clock.Second(), clock.Nanosecond(), time.UTC), !v.IsZero()
	default:
		panic("unhandled temporal validation type")
	}
}
