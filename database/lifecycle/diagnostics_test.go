package lifecycle_test

import (
	"database/sql/driver"
	"errors"
	"fmt"
	"runtime"
	"sync/atomic"
	"testing"

	"github.com/weiloon1234/Foundry-Go/database/codec"
	"github.com/weiloon1234/Foundry-Go/database/lifecycle"
	"github.com/weiloon1234/Foundry-Go/fault"
	"github.com/weiloon1234/Foundry-Go/internal/safeerror"
	"github.com/weiloon1234/Foundry-Go/value"
)

type comparisonPrivateError struct {
	calls atomic.Int32
	exit  bool
}

func (e *comparisonPrivateError) Error() string {
	e.calls.Add(1)
	if e.exit {
		runtime.Goexit()
	}
	panic("private comparison")
}
func (e *comparisonPrivateError) Format(fmt.State, rune) { _ = e.Error() }

func TestComparisonDiagnosticBoundary(t *testing.T) {
	for _, original := range []bool{true, false} {
		for _, marked := range []bool{true, false} {
			for _, exit := range []bool{true, false} {
				t.Run(fmt.Sprintf("original=%t/marked=%t/exit=%t", original, marked, exit), func(t *testing.T) {
					cause := &comparisonPrivateError{exit: exit}
					var input error = cause
					if marked {
						input = safeerror.WrapFault(fault.Invalid, "owned comparison", cause)
					}
					c := codec.New(func(v int) (driver.Value, error) {
						if (original && v == 1) || (!original && v == 2) {
							return nil, input
						}
						return int64(v), nil
					}, func(any) (int, error) { return 0, nil })
					_, err := lifecycle.CompareField(c, value.Set(1), value.Set(2), true)
					operation := "compare resulting model field"
					if original {
						operation = "compare original model field"
					}
					expected := operation + " failed"
					if marked {
						expected = operation + ": database value binding: invalid: owned comparison"
					}
					var found *comparisonPrivateError
					if err == nil || err.Error() != expected || !errors.Is(err, input) || !errors.As(err, &found) || found != cause || errors.Is(err, fault.Invalid) != marked || cause.calls.Load() != 0 {
						t.Fatal("comparison lost diagnostic, classification, identity, or formatted cause")
					}
				})
			}
		}
	}
}
