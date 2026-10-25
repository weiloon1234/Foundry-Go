package pubsub

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

type privateHandlerDiagnostic struct {
	exit  bool
	calls atomic.Int32
}

func (e *privateHandlerDiagnostic) Error() string {
	e.calls.Add(1)
	if e.exit {
		runtime.Goexit()
	}
	panic("private handler")
}
func (e *privateHandlerDiagnostic) Format(fmt.State, rune) { _ = e.Error() }

func TestSupervisedHandlerDiagnosticNeverBecomesRetryable(t *testing.T) {
	for _, operation := range []string{"pub/sub gap handler", "pub/sub supervised receiver"} {
		for _, marked := range []bool{false, true} {
			for _, exit := range []bool{false, true} {
				t.Run(fmt.Sprintf("%s/marked=%t/exit=%t", operation, marked, exit), func(t *testing.T) {
					cause := &privateHandlerDiagnostic{exit: exit}
					var input error = cause
					if marked {
						input = safeerror.WrapFault(fault.Invalid, "owned handler diagnostic", cause)
					}
					err := failedHandler(callback.Invoke(operation, func() error { return input }))
					want := "pub/sub supervised handler failed"
					if marked {
						want = operation + ": invalid: owned handler diagnostic"
					}
					var handler handlerFailure
					var retained *privateHandlerDiagnostic
					if err.Error() != want || interrupted(err) || !errors.Is(err, input) || !errors.As(err, &handler) || !errors.As(err, &retained) || retained != cause || errors.Is(err, fault.Invalid) != marked {
						t.Fatal("handler diagnostic changed message, identity, classification, or retry policy")
					}
					_ = fmt.Sprintf("%v %+v %#v", err, err, err)
					if cause.calls.Load() != 0 {
						t.Fatal("handler cause formatted")
					}
				})
			}
		}
	}
}
