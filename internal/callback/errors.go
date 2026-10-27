package callback

import (
	"github.com/weiloon1234/Foundry-Go/fault"
	"github.com/weiloon1234/Foundry-Go/internal/errorgraph"
	"github.com/weiloon1234/Foundry-Go/internal/safeerror"
	"reflect"
)

// ErrorIs bounds and contains application matching methods. Incomplete or
// failed inspection establishes no match; the original error remains a failure.
func ErrorIs(err, target error) bool {
	matched, failed := ErrorIsResult(err, target)
	return failed == nil && matched
}

// ErrorIsResult distinguishes a completed nonmatch from failed inspection.
func ErrorIsResult(err, target error) (matched bool, failed error) {
	if err == nil || target == nil {
		return err == target, nil
	}
	failed = inspectError(err, func(current error) bool {
		matched = errorgraph.Matches(current, target)
		return !matched
	})
	return failed == nil && matched, failed
}

func inspectError(err error, visit func(error) bool) error {
	complete := false
	failed := Isolated("error inspection", func() error {
		complete = errorgraph.Walk(err, visit)
		return nil
	})
	if failed != nil {
		return safeerror.WrapFault(fault.Panicked, "error inspection failed", safeerror.Join("error inspection failed", failed, err))
	}
	if !complete {
		return safeerror.WrapFault(fault.Internal, "error inspection exceeded its bounds", err)
	}
	return nil
}

// ErrorAs publishes a match only after inspection returned normally. The target
// is untouched on panic, Goexit, exhaustion, a nonmatch or a nil match.
// A nil detail cannot establish a framework classification.
func ErrorAs(err error, target any) bool {
	matched, failed := ErrorAsResult(err, target)
	return failed == nil && matched
}

// ErrorAsResult publishes a match only after completed inspection, and retains
// failed inspection separately so callers cannot normalize it as bad input.
func ErrorAsResult(err error, target any) (bool, error) {
	if err == nil {
		return false, nil
	}
	slot := reflect.ValueOf(target)
	if slot.Kind() != reflect.Pointer || slot.IsNil() {
		return false, nil
	}
	temporary := reflect.New(slot.Elem().Type())
	matched := false
	failed := inspectError(err, func(current error) bool {
		matched = errorgraph.AsTargetShallow(current, temporary.Interface())
		return !matched
	})
	if failed != nil || !matched {
		return false, failed
	}
	value := temporary.Elem()
	if value.Kind() == reflect.Interface && !value.IsNil() {
		value = value.Elem()
	}
	switch value.Kind() {
	case reflect.Pointer, reflect.Interface, reflect.Map, reflect.Slice, reflect.Func, reflect.Chan:
		if value.IsNil() {
			return false, safeerror.WrapFault(fault.Internal, "error inspection returned a nil detail", err)
		}
	}
	slot.Elem().Set(temporary.Elem())
	return true, nil
}

// ErrorWalk contains a bounded graph walk. Visitors must treat their collected
// results as provisional until this returns true.
func ErrorWalk(err error, visit func(error) bool) bool {
	if err == nil {
		return true
	}
	complete := false
	failed := Isolated("error classification", func() error { complete = errorgraph.Walk(err, visit); return nil })
	return failed == nil && complete
}
