package http

import (
	"errors"
	"fmt"
	"testing"

	"github.com/weiloon1234/Foundry-Go/internal/callback"
)

func TestHTTPResponseDiagnosticsSurviveContainment(t *testing.T) {
	for _, exit := range []bool{false, true} {
		for _, code := range []ErrorCode{BadRequest, Forbidden, InternalError, "private custom code"} {
			t.Run(fmt.Sprintf("%s/goexit=%t", code, exit), func(t *testing.T) {
				cause := &privateTransportSourceDiagnostic{exit: exit}
				original := code.WithCause(cause)
				result := callback.Isolated("response", func() error { return original })
				var retained *privateTransportSourceDiagnostic
				var typed *responseError
				if !errors.Is(result, original) || !errors.Is(result, cause) || !errors.Is(result, code) || !errors.As(result, &retained) || retained != cause || !errors.As(result, &typed) || !errors.Is(result, typed) {
					t.Fatal("response lost typed classification or cause identity")
				}
				want := "response: " + string(code)
				if code == "private custom code" {
					want = "response: internal_error"
				}
				*typed = responseError{code: "private replacement", cause: cause}
				for _, format := range []string{"%v", "%+v", "%#v", "%s"} {
					if got := fmt.Sprintf(format, result); got != want {
						t.Fatalf("response diagnostic = %q, want %q", got, want)
					}
				}
				if cause.calls.Load() != 0 {
					t.Fatal("response formatted an application cause")
				}
			})
		}
	}
}
