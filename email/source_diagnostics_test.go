package email_test

import (
	"errors"
	"fmt"
	"runtime"
	"sync/atomic"
	"testing"

	"github.com/weiloon1234/Foundry-Go/email"
	"github.com/weiloon1234/Foundry-Go/foundation"
	"github.com/weiloon1234/Foundry-Go/internal/callback"
	"github.com/weiloon1234/Foundry-Go/internal/emaildiag"
)

func TestEmailSourceClassificationDiagnosticsSurviveContainment(t *testing.T) {
	for _, kind := range []email.Kind{email.Construction, email.Permanent, email.Transient, email.Ambiguous, "private application classification"} {
		t.Run(string(kind), func(t *testing.T) {
			original := emaildiag.Failure(kind)
			result := callback.Isolated("email operation", func() error { return original })
			want := "email operation: " + string(kind)
			if kind == "private application classification" {
				want = "email operation failed"
			}
			var retained email.Kind
			if !errors.Is(result, original) || !errors.Is(result, kind) || !errors.As(result, &retained) || retained != kind {
				t.Fatal("email classification lost source identity")
			}
			for _, format := range []string{"%v", "%+v", "%#v", "%s"} {
				if got := fmt.Sprintf(format, result); got != want {
					t.Fatalf("email classification diagnostic = %q, want %q", got, want)
				}
			}
		})
	}
}

type privateEmailConstructorDiagnostic struct {
	exit  bool
	calls atomic.Int32
}

func (e *privateEmailConstructorDiagnostic) Error() string {
	e.calls.Add(1)
	if e.exit {
		runtime.Goexit()
	}
	panic("private email constructor diagnostic")
}
func (e *privateEmailConstructorDiagnostic) Format(fmt.State, rune) { _ = e.Error() }

func TestEmailModuleConstructionDiagnosticSurvivesFoundation(t *testing.T) {
	key := foundation.NewKey[*email.Mailer]("mailer")
	for _, stage := range []string{"nil constructor", "nil mailer", "hostile panic", "hostile goexit"} {
		t.Run(stage, func(t *testing.T) {
			cause := &privateEmailConstructorDiagnostic{exit: stage == "hostile goexit"}
			private := errors.Join(email.Transient, cause)
			var construct func(foundation.Resolver) (*email.Mailer, error)
			want := "register provider mail: email construction failed"
			if stage == "nil mailer" {
				construct = func(foundation.Resolver) (*email.Mailer, error) { return nil, nil }
				want = "construct service mailer: email construction failed"
			} else if stage == "hostile panic" || stage == "hostile goexit" {
				construct = func(foundation.Resolver) (*email.Mailer, error) { return nil, private }
				want = "construct service mailer failed"
			}
			_, err := foundation.NewBuilder().Register(email.Module("mail", key, nil, construct)).Build(t.Context())
			if err == nil {
				t.Fatal("invalid email constructor succeeded")
			}
			if stage == "hostile panic" || stage == "hostile goexit" {
				var retained *privateEmailConstructorDiagnostic
				if !errors.Is(err, private) || !errors.Is(err, cause) || !errors.Is(err, email.Transient) || !errors.As(err, &retained) || retained != cause {
					t.Fatal("email construction lost private cause identity or known classification")
				}
			} else if !errors.Is(err, email.Construction) {
				t.Fatal("email construction lost builtin classification")
			}
			if err.Error() != want {
				t.Fatal("email module lost its framework construction diagnostic:", err)
			}
			for _, format := range []string{"%v", "%+v", "%#v", "%s"} {
				if got := fmt.Sprintf(format, err); got != want {
					t.Fatalf("email module diagnostic = %q, want %q", got, want)
				}
			}
			if cause.calls.Load() != 0 {
				t.Fatal("email construction formatted an application error")
			}
		})
	}
}
