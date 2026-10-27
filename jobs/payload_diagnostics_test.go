package jobs

import (
	"context"
	"errors"
	"fmt"
	"testing"

	"github.com/weiloon1234/Foundry-Go/internal/callback"
)

func TestPayloadDiagnosticsSurvivePreparationContainment(t *testing.T) {
	definition := Define[middlewareDiagnosticPayload]("diagnostic.payload", 1, DefaultPolicy("default"))
	declaration, err := definition.Declare(func(context.Context, middlewareDiagnosticPayload) error { return nil })
	if err != nil {
		t.Fatal(err)
	}
	pending, err := definition.Capture(t.Context(), middlewareDiagnosticPayload{}, Options[middlewareDiagnosticPayload]{})
	if err != nil {
		t.Fatal(err)
	}
	for _, stage := range []string{"malformed", "wrong type", "hostile panic formatter", "hostile goexit formatter"} {
		t.Run(stage, func(t *testing.T) {
			cause := &privateMiddlewareDiagnostic{exit: stage == "hostile goexit formatter"}
			var original error
			result := callback.Isolated("prepare job", func() error {
				if stage == "hostile panic formatter" || stage == "hostile goexit formatter" {
					original = payloadFailure(cause)
					return original
				}
				envelope := pending.Envelope()
				envelope.payload = "{"
				if stage == "wrong type" {
					envelope.payload = `{"number":"private value"}`
				}
				_, err := declaration.prepare(t.Context(), envelope)
				original = err
				return err
			})
			var source *payloadError
			if original == nil || !errors.Is(result, original) || !errors.As(result, &source) || !errors.Is(result, source) {
				t.Fatal("payload preparation lost source classification or identity")
			}
			if stage == "hostile panic formatter" || stage == "hostile goexit formatter" {
				var retained *privateMiddlewareDiagnostic
				if !errors.Is(result, cause) || !errors.As(result, &retained) || retained != cause {
					t.Fatal("payload preparation lost its private cause")
				}
			}
			for _, format := range []string{"%v", "%+v", "%#v", "%s"} {
				if got := fmt.Sprintf(format, result); got != "prepare job: job payload does not match its registered schema" {
					t.Fatalf("payload diagnostic = %q", got)
				}
			}
			if cause.calls.Load() != 0 {
				t.Fatal("payload preparation formatted an application cause")
			}
		})
	}
}
