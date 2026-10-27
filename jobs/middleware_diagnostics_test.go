package jobs

import (
	"context"
	"errors"
	"fmt"
	"runtime"
	"sync/atomic"
	"testing"

	"github.com/weiloon1234/Foundry-Go/database/codec"
	"github.com/weiloon1234/Foundry-Go/fault"
	"github.com/weiloon1234/Foundry-Go/internal/callback"
)

type middlewareDiagnosticPayload struct {
	Number int `json:"number"`
}

type privateMiddlewareDiagnostic struct {
	exit  bool
	calls atomic.Int32
}

func (e *privateMiddlewareDiagnostic) Error() string {
	e.calls.Add(1)
	if e.exit {
		runtime.Goexit()
	}
	panic("private middleware diagnostic")
}

func (e *privateMiddlewareDiagnostic) Format(fmt.State, rune) { _ = e.Error() }
func (*privateMiddlewareDiagnostic) Unwrap() error            { return fault.Invalid }

func TestPreparedJobRetainsDiagnosticsAcrossMiddlewareAggregation(t *testing.T) {
	for _, exit := range []bool{false, true} {
		for _, stage := range []string{"handler", "after", "failed sibling", "private handler", "private after"} {
			t.Run(fmt.Sprintf("%s/goexit=%t", stage, exit), func(t *testing.T) {
				cause := &privateMiddlewareDiagnostic{exit: exit}
				var uninitialized codec.Codec[int]
				original := uninitialized.Validate()
				var raw *fault.Error
				if !errors.As(original, &raw) {
					t.Fatal("codec validation did not retain its fault")
				}
				handler := Handler[middlewareDiagnosticPayload](func(context.Context, middlewareDiagnosticPayload) error { return nil })
				middleware := Middleware[middlewareDiagnosticPayload]{Failed: func(context.Context, middlewareDiagnosticPayload, error) error { return nil }}
				want := "execute job: job handler: invalid: invalid database codec value or destination"
				private := false
				switch stage {
				case "handler", "failed sibling":
					handler = func(context.Context, middlewareDiagnosticPayload) error { return original }
					if stage == "failed sibling" {
						middleware.Failed = func(context.Context, middlewareDiagnosticPayload, error) error { return cause }
						want += "\noperation failed"
					}
				case "after":
					middleware.After = func(context.Context, middlewareDiagnosticPayload) error { return original }
					want = "execute job: after job: invalid: invalid database codec value or destination"
				case "private handler":
					handler = func(context.Context, middlewareDiagnosticPayload) error { return cause }
					want, private = "execute job failed", true
				case "private after":
					middleware.After = func(context.Context, middlewareDiagnosticPayload) error { return cause }
					want, private = "execute job failed", true
				}
				definition := Define[middlewareDiagnosticPayload]("diagnostic.middleware", 1, DefaultPolicy("default"))
				declaration, err := definition.DeclareWith(handler, HandlerOptions[middlewareDiagnosticPayload]{Middleware: []Middleware[middlewareDiagnosticPayload]{middleware}})
				if err != nil {
					t.Fatal(err)
				}
				pending, err := definition.Capture(t.Context(), middlewareDiagnosticPayload{Number: 7}, Options[middlewareDiagnosticPayload]{})
				if err != nil {
					t.Fatal(err)
				}
				prepared, err := declaration.prepare(t.Context(), pending.Envelope())
				if err != nil {
					t.Fatal(err)
				}
				if prepared.invoke == nil {
					t.Fatal("declaration did not prepare its handler")
				}
				result := callback.Isolated("execute job", func() error { return prepared.invoke(t.Context()) })
				if result == nil || !errors.Is(result, fault.Invalid) {
					t.Fatal("prepared execution lost failure classification")
				}
				if private {
					var retained *privateMiddlewareDiagnostic
					if !errors.Is(result, cause) || !errors.As(result, &retained) || retained != cause {
						t.Fatal("prepared execution lost the private cause")
					}
				} else {
					var retained *fault.Error
					if !errors.Is(result, original) || !errors.As(result, &retained) || retained != raw {
						t.Fatal("prepared execution lost the source fault")
					}
					if stage == "failed sibling" && !errors.Is(result, cause) {
						t.Fatal("middleware aggregation lost the sibling failure")
					}
					*raw = *fault.Wrap(fault.Invalid, "private replacement", cause)
				}
				for _, format := range []string{"%v", "%+v", "%#v", "%s"} {
					if got := fmt.Sprintf(format, result); got != want {
						t.Fatalf("prepared execution diagnostic = %q, want %q", got, want)
					}
				}
				if cause.calls.Load() != 0 {
					t.Fatal("prepared execution formatted an application cause")
				}
			})
		}
	}
}
