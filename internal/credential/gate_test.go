package credential

import (
	"context"
	"errors"
	"fmt"
	"runtime"
	"strings"
	"testing"
	"time"

	"github.com/weiloon1234/Foundry-Go/database"
	"github.com/weiloon1234/Foundry-Go/fault"
)

func TestLateCancellationRetainsOperationalCauseWithoutDisclosure(t *testing.T) {
	ctx, cancel := context.WithCancel(t.Context())
	defer cancel()
	cause := errors.New("private backend failure")
	err := NewGate(1, time.Second).Execute(ctx, func(context.Context) error { cancel(); return cause })
	if !errors.Is(err, context.Canceled) || !errors.Is(err, cause) {
		t.Fatal("cancellation erased operation cause", err)
	}
	if strings.Contains(err.Error(), cause.Error()) {
		t.Fatal("private cause disclosed")
	}
}

type cyclicFailure struct{}

func (e *cyclicFailure) Error() string { return "private-failure-sentinel" }
func (e *cyclicFailure) Unwrap() error { return e }

type inspectingFailure struct{ inspect func() }

func (e inspectingFailure) Error() string { return "private-failure-sentinel" }
func (e inspectingFailure) Unwrap() error { e.inspect(); return nil }

type unknownOutcome struct{}

func (unknownOutcome) Error() string        { return "private-failure-sentinel" }
func (unknownOutcome) Is(target error) bool { return target == database.CommitUnknown }

func TestCredentialFailureClassificationIsBoundedAndRedacted(t *testing.T) {
	overloaded := fault.Wrap(fault.Overloaded, "capacity exhausted", errors.New("private-failure-sentinel"))
	for _, test := range []struct {
		name  string
		cause error
		code  fault.Code
	}{
		{"capacity", overloaded, fault.Overloaded},
		{"deadline", context.DeadlineExceeded, fault.Timeout},
		{"unknown-before-capacity", errors.Join(database.CommitUnknown, overloaded), fault.Internal},
		{"unknown-after-capacity", errors.Join(overloaded, database.CommitUnknown), fault.Internal},
		{"custom-unknown", errors.Join(overloaded, unknownOutcome{}), fault.Internal},
		{"cycle", errors.Join(overloaded, &cyclicFailure{}), fault.Internal},
		{"panic", errors.Join(overloaded, inspectingFailure{func() { panic("private-failure-sentinel") }}), fault.Internal},
		{"goexit", errors.Join(overloaded, inspectingFailure{runtime.Goexit}), fault.Internal},
	} {
		t.Run(test.name, func(t *testing.T) {
			calls := 0
			err := NewGate(1, time.Second).Execute(t.Context(), func(context.Context) error { calls++; return test.cause })
			classified, ok := err.(*fault.Error)
			if calls != 1 || !ok || classified.Code() != test.code {
				t.Fatal("incorrect classification or replay", calls, err)
			}
			for _, format := range []string{"%v", "%+v", "%#v"} {
				if strings.Contains(fmt.Sprintf(format, err), "private-failure-sentinel") {
					t.Fatal("failure disclosed private cause")
				}
			}
		})
	}
}

type asFailure struct{ inspect func(any) bool }

func (asFailure) Error() string        { return "private-as-sentinel" }
func (e asFailure) As(target any) bool { return e.inspect(target) }

func TestCredentialFailureCustomAsFailsClosed(t *testing.T) {
	for name, inspect := range map[string]func(any) bool{
		"typed-nil": func(target any) bool {
			if slot, ok := target.(**database.Error); ok {
				*slot = nil
				return true
			}
			return false
		},
		"panic":  func(any) bool { panic("private-as-sentinel") },
		"goexit": func(any) bool { runtime.Goexit(); return false },
	} {
		t.Run(name, func(t *testing.T) {
			opaque := asFailure{inspect}
			overloaded := fault.New(fault.Overloaded, "capacity exhausted")
			for _, cause := range []error{errors.Join(opaque, overloaded), errors.Join(overloaded, opaque)} {
				calls := 0
				err := NewGate(1, time.Second).Execute(t.Context(), func(context.Context) error { calls++; return cause })
				classified, ok := err.(*fault.Error)
				if calls != 1 || !ok || classified.Code() != fault.Internal {
					t.Fatal("unsafe As result was classified optimistically", calls, err)
				}
				for _, format := range []string{"%v", "%+v", "%#v"} {
					if strings.Contains(fmt.Sprintf(format, err), "private-as-sentinel") {
						t.Fatal("As failure disclosed private data")
					}
				}
			}
		})
	}
}
