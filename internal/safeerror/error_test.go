package safeerror

import (
	"errors"
	"fmt"
	"testing"

	"github.com/weiloon1234/Foundry-Go/fault"
)

type privateError struct{}

func TestDiagnosticRetainsCauseAndHasConcreteBoundary(t *testing.T) {
	cause := &privateError{}
	err := Diagnostic("safe", cause)
	var retained *privateError
	if !IsDiagnostic(err) || !errors.Is(err, cause) || !errors.As(err, &retained) || retained != cause {
		t.Fatal("diagnostic cause lost")
	}
	if IsDiagnostic(Wrap("safe", cause)) || Diagnostic("safe", nil) != nil {
		t.Fatal("neutral or nil wrapper became a diagnostic boundary")
	}
}

func (*privateError) Error() string          { panic("private") }
func (*privateError) Format(fmt.State, rune) { panic("private formatter") }
func TestWrapPreservesCauseWithoutClassificationOrFormatting(t *testing.T) {
	private := &privateError{}
	for _, cause := range []error{private, fault.Wrap(fault.Invalid, "invalid value", private), fault.Wrap(fault.Internal, "infrastructure", private)} {
		err := Wrap("safe", cause)
		for _, code := range []fault.Code{fault.Invalid, fault.Internal, fault.Panicked} {
			if errors.Is(err, code) != errors.Is(cause, code) {
				t.Fatal("redaction added classification")
			}
		}
		var retained *privateError
		if !errors.Is(err, private) || !errors.As(err, &retained) || retained != private {
			t.Fatal("cause lost")
		}
		if fmt.Sprintf("%v %+v %#v", err, err, err) != "safe safe safe" {
			t.Fatal("unsafe formatting")
		}
	}
	if Wrap("safe", nil) != nil {
		t.Fatal("nil became failure")
	}
}
