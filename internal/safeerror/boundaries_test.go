package safeerror_test

import (
	"context"
	"errors"
	"fmt"
	"runtime"
	"sync/atomic"
	"testing"
	"time"

	"github.com/weiloon1234/Foundry-Go/fault"
	"github.com/weiloon1234/Foundry-Go/internal/callback"
	"github.com/weiloon1234/Foundry-Go/internal/faultwrap"
	"github.com/weiloon1234/Foundry-Go/internal/frameworkadapter"
	"github.com/weiloon1234/Foundry-Go/internal/safeerror"
	"github.com/weiloon1234/Foundry-Go/internal/sqlcallback"
	"github.com/weiloon1234/Foundry-Go/internal/workscope"
)

type hostileDiagnosticCause struct {
	exit  bool
	calls atomic.Int32
}

func (e *hostileDiagnosticCause) Error() string {
	e.calls.Add(1)
	if e.exit {
		runtime.Goexit()
	}
	panic("private diagnostic")
}
func (e *hostileDiagnosticCause) Format(fmt.State, rune) { _ = e.Error() }

func TestContainmentBoundaryDiagnosticContract(t *testing.T) {
	group, err := workscope.New(1, time.Second)
	if err != nil {
		t.Fatal(err)
	}
	defer group.Close(context.Background())
	for _, boundary := range []struct {
		name    string
		apply   func(error) error
		marked  string
		private string
	}{
		{"Invoke", func(e error) error { return callback.Invoke("operation", func() error { return e }) }, "operation: invalid: owned diagnostic", "operation failed"},
		{"Isolated", func(e error) error { return callback.Isolated("operation", func() error { return e }) }, "operation: invalid: owned diagnostic", "operation failed"},
		{"admission", func(e error) error {
			return group.Run(t.Context(), "operation", func(context.Context) error { return e })
		}, "operation: invalid: owned diagnostic", "operation failed"},
		{"adapter", func(e error) error { return frameworkadapter.Call(nil, "operation", func() error { return e }) }, "operation: invalid: owned diagnostic", "operation failed"},
		{"operation prefix", func(e error) error { return safeerror.Prefix("operation", e) }, "operation: invalid: owned diagnostic", "operation failed"},
		{"neutral wrapper", func(e error) error { return safeerror.Wrap("operation failed", e) }, "invalid: owned diagnostic", "operation failed"},
		{"generic classified failure", func(e error) error { return safeerror.WrapFailure(fault.Internal, "operation failed", e) }, "invalid: owned diagnostic", "internal: operation failed"},
		{"native wrapper recovery", func(e error) error {
			foreign := fmt.Errorf("native context: %w", safeerror.Seal("conversion failed", e))
			return callback.RetainDiagnostic(foreign, safeerror.Reveal("operation failed", foreign))
		}, "invalid: owned diagnostic", "operation failed"},
		{"classified wrapper", func(e error) error { return faultwrap.Wrap("operation failed", e) }, "invalid: owned diagnostic", "invalid: operation failed"},
		{"foreign capsule", func(e error) error {
			return safeerror.Reveal("operation failed", safeerror.Seal("conversion failed", e))
		}, "invalid: owned diagnostic", "operation failed"},
		{"foreign sentinel capsule", func(e error) error {
			return safeerror.Reveal("operation failed", safeerror.SealMatching("conversion failed", e, errors.ErrUnsupported))
		}, "invalid: owned diagnostic", "operation failed"},
		{"SQL callback", func(e error) error {
			return safeerror.Reveal("operation failed", sqlcallback.Invoke("convert", func() error { return e }))
		}, "convert: invalid: owned diagnostic", "operation failed"},
		{"cleanup join", func(e error) error {
			return callback.Isolated("operation", func() error { return safeerror.Join("cleanup failed", e, errors.New("private cleanup")) })
		}, "operation: invalid: owned diagnostic\ncleanup failed", "operation failed"},
	} {
		for _, owned := range []bool{true, false} {
			for _, exit := range []bool{false, true} {
				t.Run(fmt.Sprintf("%s/owned=%t/goexit=%t", boundary.name, owned, exit), func(t *testing.T) {
					cause := &hostileDiagnosticCause{exit: exit}
					raw := fault.Wrap(fault.Invalid, "private application diagnostic", cause)
					var input error = raw
					want := boundary.private
					if owned {
						input = safeerror.WrapFault(fault.Invalid, "owned diagnostic", cause)
						want = boundary.marked
						errors.As(input, &raw)
					}
					result := boundary.apply(input)
					var retained *hostileDiagnosticCause
					if result == nil || result.Error() != want || !errors.Is(result, input) || !errors.Is(result, raw) || !errors.Is(result, cause) || !errors.As(result, &retained) || retained != cause || !errors.Is(result, fault.Invalid) {
						t.Fatal("boundary lost diagnostic, classification or identity", result)
					}
					*raw = *fault.Wrap(fault.Invalid, "private mutation", cause)
					if got := fmt.Sprintf("%v %+v %#v", result, result, result); got != want+" "+want+" "+want || cause.calls.Load() != 0 {
						t.Fatal("boundary formatted application text")
					}
				})
			}
		}
	}
}

func TestDiscoveredApplicationFaultCodeIsNotDiagnosticText(t *testing.T) {
	cause := &hostileDiagnosticCause{}
	code := fault.Code("private classification")
	raw := fault.Wrap(code, "private message", cause)
	result := faultwrap.Wrap("operation failed", raw)
	if result.Error() != "internal: operation failed" || !errors.Is(result, raw) || !errors.Is(result, code) || !errors.Is(result, cause) || cause.calls.Load() != 0 {
		t.Fatal("untrusted classification text escaped")
	}
}
