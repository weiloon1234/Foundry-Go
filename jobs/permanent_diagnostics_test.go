package jobs_test

import (
	"errors"
	"fmt"
	"testing"

	"github.com/weiloon1234/Foundry-Go/database/codec"
	"github.com/weiloon1234/Foundry-Go/fault"
	"github.com/weiloon1234/Foundry-Go/internal/callback"
	"github.com/weiloon1234/Foundry-Go/jobs"
)

func TestPermanentDiagnosticSurvivesHandlerContainment(t *testing.T) {
	for _, exit := range []bool{false, true} {
		for _, marked := range []bool{false, true} {
			t.Run(fmt.Sprintf("marked=%t/goexit=%t", marked, exit), func(t *testing.T) {
				cause := &privateJobConstructorDiagnostic{exit: exit}
				var input error = cause
				if marked {
					var uninitialized codec.Codec[int]
					input = uninitialized.Validate()
					var raw *fault.Error
					if !errors.As(input, &raw) {
						t.Fatal("framework input lost its typed fault")
					}
					*raw = *fault.Wrap(fault.Invalid, "private replacement", cause)
				}
				original := jobs.Permanent(input)
				result := callback.Isolated("job handler", func() error { return original })
				var retained *privateJobConstructorDiagnostic
				if !errors.Is(result, original) || !errors.Is(result, input) || !errors.Is(result, cause) || !errors.Is(result, fault.Invalid) || !errors.As(result, &retained) || retained != cause {
					t.Fatal("permanent failure lost classification or cause identity")
				}
				want := "job handler: job failed without automatic retry"
				for _, format := range []string{"%v", "%+v", "%#v", "%s"} {
					if got := fmt.Sprintf(format, result); got != want {
						t.Fatalf("permanent diagnostic = %q, want %q", got, want)
					}
				}
				if cause.calls.Load() != 0 {
					t.Fatal("permanent failure formatted an application cause")
				}
			})
		}
	}
}
