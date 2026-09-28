// Package errorgraph bounds framework-owned inspection of wrapped/joined errors.
// Callers isolate custom Is/As/Unwrap methods and retain callback ownership.
package errorgraph

import "reflect"

// Walk visits non-nil errors in depth-first order. A false visitor result stops
// successfully. False means traversal exceeded its node/depth bounds. A callback
// must return; this bounds graph traversal, not arbitrary work inside methods.
func Walk(err error, visit func(error) bool) bool {
	type frame struct {
		err   error
		depth int
	}
	pending := []frame{{err: err}}
	for remaining := 256; len(pending) > 0; remaining-- {
		if remaining == 0 {
			return false
		}
		current := pending[len(pending)-1]
		pending = pending[:len(pending)-1]
		if current.depth > 64 {
			return false
		}
		if current.err == nil {
			continue
		}
		if !visit(current.err) {
			return true
		}
		switch wrapped := current.err.(type) {
		case interface{ Unwrap() error }:
			pending = append(pending, frame{wrapped.Unwrap(), current.depth + 1})
		case interface{ Unwrap() []error }:
			children := wrapped.Unwrap()
			if len(children) > remaining-1-len(pending) {
				return false
			}
			for i := len(children) - 1; i >= 0; i-- {
				pending = append(pending, frame{children[i], current.depth + 1})
			}
		}
	}
	return true
}

// Matches performs the equality/custom-Is step of errors.Is without unwrapping.
// Use inside Walk to retain one shared traversal budget across classifications.
func Matches(err, target error) bool {
	if err == nil || target == nil {
		return err == target
	}
	if reflect.TypeOf(target).Comparable() && err == target {
		return true
	}
	match, ok := err.(interface{ Is(error) bool })
	return ok && match.Is(target)
}

// AsShallow performs the assignability/custom-As step of errors.As without
// unwrapping. T must implement error. Use inside Walk to bound the whole search.
func AsShallow[T error](err error) (T, bool) {
	if value, ok := err.(T); ok {
		return value, true
	}
	var value T
	if match, ok := err.(interface{ As(any) bool }); ok && match.As(&value) {
		return value, true
	}
	return value, false
}

// Is searches for equality or a custom Is match within Walk's shared bounds.
// Exhaustion means no match was established. Callers retain isolation and must
// treat an unmatched non-nil error as a failure, not evidence of successful work.
func Is(err, target error) bool {
	if err == nil || target == nil {
		return err == target
	}
	matched := false
	complete := Walk(err, func(current error) bool {
		matched = Matches(current, target)
		return !matched
	})
	return complete && matched
}

// As returns the first assignable/custom-As match in depth-first order. Complete
// distinguishes an absent match from an exhausted traversal. A found value ends
// the search before unwrapping it, including when the matched value is typed nil.
func As[T error](err error) (value T, found, complete bool) {
	complete = Walk(err, func(current error) bool {
		value, found = AsShallow[T](current)
		return !found
	})
	return value, found, complete
}

// Has searches for an assignable/custom-As match within Walk's shared bounds.
// An incomplete search establishes no match.
func Has[T error](err error) bool {
	_, found, complete := As[T](err)
	return complete && found
}
