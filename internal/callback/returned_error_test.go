package callback_test

import (
	"errors"
	"fmt"
	"runtime"
	"sync/atomic"
	"testing"

	"github.com/weiloon1234/Foundry-Go/fault"
	"github.com/weiloon1234/Foundry-Go/internal/callback"
	"github.com/weiloon1234/Foundry-Go/internal/safeerror"
)

type hostileReturnedError struct {
	mode  string
	calls atomic.Int32
}

func (e *hostileReturnedError) Error() string {
	e.calls.Add(1)
	switch e.mode {
	case "panic":
		panic("private error")
	case "exit":
		runtime.Goexit()
	}
	return "private error"
}
func (e *hostileReturnedError) Format(fmt.State, rune) { _ = e.Error() }
func (e *hostileReturnedError) Is(error) bool          { panic("unexpected inspection") }
func (e *hostileReturnedError) As(any) bool            { runtime.Goexit(); return false }
func (e *hostileReturnedError) Unwrap() error          { return e }

func TestReturnedErrorMethodsNeverRunAtCallbackBoundary(t *testing.T) {
	for _, mode := range []string{"private", "panic", "exit"} {
		t.Run(mode, func(t *testing.T) {
			cause := &hostileReturnedError{mode: mode}
			for _, invoke := range []func(string, func() error) error{callback.Invoke, callback.Isolated} {
				err := invoke("extension", func() error { return cause })
				var retained *hostileReturnedError
				if !errors.Is(err, cause) || !errors.As(err, &retained) || retained != cause {
					t.Fatal("lost original cause")
				}
				for _, format := range []string{"%v", "%+v", "%#v", "%s", "%q"} {
					_ = fmt.Sprintf(format, err)
				}
				if cause.calls.Load() != 0 {
					t.Fatal("application formatter executed")
				}
			}
		})
	}
}

func TestOwnedDiagnosticSurvivesNestedContainmentAndCauseMutation(t *testing.T) {
	for _, mode := range []string{"private", "panic", "exit"} {
		t.Run(mode, func(t *testing.T) {
			cause := &hostileReturnedError{mode: mode}
			original := fault.New(fault.Invalid, "framework diagnostic")
			marked := safeerror.Owned(original.Error(), original)
			*original = *fault.Wrap(fault.Invalid, "private replacement", cause)
			err := callback.Isolated("outer", func() error {
				return callback.Invoke("inner", func() error { return marked })
			})
			var retained *hostileReturnedError
			var retainedFault *fault.Error
			if !errors.Is(err, marked) || !errors.Is(err, cause) || !errors.Is(err, fault.Invalid) || !errors.As(err, &retained) || retained != cause || !errors.As(err, &retainedFault) || retainedFault != original {
				t.Fatal("owned diagnostic lost classification or identity")
			}
			want := "outer: inner: invalid: framework diagnostic"
			for _, format := range []string{"%v", "%+v", "%#v", "%s"} {
				if fmt.Sprintf(format, err) != want {
					t.Fatal("owned diagnostic changed or exposed application text")
				}
			}
			if cause.calls.Load() != 0 {
				t.Fatal("owned diagnostic invoked application formatting")
			}
		})
	}
}

func TestApplicationFaultDoesNotBecomeOwnedDiagnostic(t *testing.T) {
	original := fault.New(fault.Invalid, "private application fault")
	err := callback.Isolated("operation", func() error { return original })
	if err.Error() != "operation failed" || !errors.Is(err, original) || !errors.Is(err, fault.Invalid) {
		t.Fatal("application fault gained trusted diagnostic text")
	}
}
