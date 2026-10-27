package auth_test

import (
	"errors"
	"fmt"
	"runtime"
	"sync/atomic"
	"testing"

	"github.com/weiloon1234/Foundry-Go/auth"
	"github.com/weiloon1234/Foundry-Go/internal/callback"
)

type privateAuthSourceDiagnostic struct {
	exit  bool
	calls atomic.Int32
}

func (e *privateAuthSourceDiagnostic) Error() string {
	e.calls.Add(1)
	if e.exit {
		runtime.Goexit()
	}
	panic("private auth diagnostic")
}
func (e *privateAuthSourceDiagnostic) Format(fmt.State, rune) { _ = e.Error() }

func TestAuthenticationSourceDiagnosticsSurviveContainment(t *testing.T) {
	for _, exit := range []bool{false, true} {
		for _, code := range []auth.Code{auth.Unauthenticated, auth.Forbidden, auth.MFARequired, auth.CredentialLimit, auth.ConfirmationRequired, auth.ImpersonationForbidden, "private custom code"} {
			t.Run(fmt.Sprintf("%s/goexit=%t", code, exit), func(t *testing.T) {
				cause := &privateAuthSourceDiagnostic{exit: exit}
				original := code.WithCause(cause)
				result := callback.Isolated("authentication", func() error { return original })
				want := "authentication: " + string(code)
				if code == "private custom code" {
					want = "authentication failed"
				}
				var retained *privateAuthSourceDiagnostic
				if !errors.Is(result, original) || !errors.Is(result, cause) || !errors.Is(result, code) || !errors.As(result, &retained) || retained != cause {
					t.Fatal("authentication source lost classification or identity")
				}
				for _, format := range []string{"%v", "%+v", "%#v", "%s"} {
					if got := fmt.Sprintf(format, result); got != want {
						t.Fatalf("authentication diagnostic = %q, want %q", got, want)
					}
				}
				if cause.calls.Load() != 0 {
					t.Fatal("authentication formatted an application cause")
				}
			})
		}
	}
	denial := auth.NewDenial("documents.closed", "private policy detail")
	result := callback.Isolated("policy", func() error { return denial })
	var retained *auth.Denial
	if !errors.Is(result, denial) || !errors.Is(result, auth.Forbidden) || !errors.As(result, &retained) || !errors.Is(result, retained) || retained.Code() != "documents.closed" || retained.Message() != "private policy detail" || result.Error() != "policy: forbidden" {
		t.Fatal("policy denial lost its fixed diagnostic or typed decision")
	}
}
