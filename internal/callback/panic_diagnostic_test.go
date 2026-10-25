package callback_test

import (
	"errors"
	"fmt"
	"testing"

	"github.com/weiloon1234/Foundry-Go/fault"
	"github.com/weiloon1234/Foundry-Go/internal/callback"
	"github.com/weiloon1234/Foundry-Go/internal/errordiag"
)

func TestPanicDiagnosticRequiresImmutableOwnerProvenance(t *testing.T) {
	for _, real := range []bool{false, true} {
		t.Run(fmt.Sprint(real), func(t *testing.T) {
			cause := &hostileReturnedError{mode: "panic"}
			var original error = fault.Wrap(fault.Panicked, "private fabricated panic", cause)
			want := "operation failed"
			if real {
				original = callback.Isolated("source", func() error { panic("private payload") })
				want = "panicked: source panicked"
			}
			var exposed *fault.Error
			if !errors.As(original, &exposed) {
				t.Fatal("missing typed fault")
			}
			if real {
				*exposed = *fault.Panic("private mutated panic", []fault.Frame{{Function: "private frame"}})
			}
			result := callback.Isolated("operation", func() error { return original })
			if result.Error() != want || !errors.Is(result, original) || !errors.Is(result, fault.Panicked) {
				t.Fatal("panic diagnostic or identity changed")
			}
			summary := errordiag.Describe(result)
			if real {
				if len(summary.Faults) != 1 || summary.Faults[0].Message != "source panicked" || len(summary.Frames) == 0 || summary.Frames[0].Function == "private frame" {
					t.Fatal("recovery snapshot lost")
				}
			} else if len(summary.Faults) != 0 || len(summary.Frames) != 0 {
				t.Fatal("application gained diagnostic authority")
			}
			_ = fmt.Sprintf("%v %+v %#v", result, result, result)
			if cause.calls.Load() != 0 {
				t.Fatal("application formatting invoked")
			}
		})
	}
}
