package http

import (
	"context"
	"errors"
	"fmt"
	"net/http/httptest"
	"runtime"
	"testing"
	"time"

	"github.com/weiloon1234/Foundry-Go/fault"
)

func TestBrowserSessionPublicationRetainsSourceDiagnostics(t *testing.T) {
	for _, stage := range []string{"expired", "mutation panic formatter", "mutation goexit formatter", "clock panic", "clock goexit"} {
		t.Run(stage, func(t *testing.T) {
			ctx, state := sessionState(t)
			cause := &privateTransportSourceDiagnostic{exit: stage == "mutation goexit formatter" || stage == "clock goexit"}
			now := time.Date(2026, time.October, 25, 12, 0, 0, 0, time.UTC)
			state.owner.clock = browserTestClock(func() time.Time {
				switch stage {
				case "clock panic":
					panic(cause)
				case "clock goexit":
					runtime.Goexit()
				}
				return now
			})
			var private error
			mutation := state.operation(ctx, func(context.Context) (string, time.Time, error) {
				if stage == "mutation panic formatter" || stage == "mutation goexit formatter" {
					private = Unauthenticated.WithCause(cause)
					return "", time.Time{}, private
				}
				return "credential=private", now, nil
			})
			writer := httptest.NewRecorder()
			result := mutation
			want := "HTTP session mutation: unauthenticated"
			if private == nil {
				if mutation != nil {
					t.Fatal("valid staged cookie was rejected:", mutation)
				}
				result = publishBrowserSession(ctx, writer, 204)
				want = "HTTP session expiry: unauthenticated"
			}
			if result == nil || writer.Header().Get("Set-Cookie") != "" {
				t.Fatal("failed session stage published a cookie")
			}
			if stage == "clock panic" || stage == "clock goexit" {
				want = "panicked: HTTP session expiry panicked"
				if stage == "clock goexit" {
					want = "panicked: HTTP session expiry exited without returning"
				}
				if !errors.Is(result, fault.Panicked) || errors.Is(result, cause) {
					t.Fatal("clock failure lost panic classification or retained a private panic payload")
				}
			} else {
				var source *responseError
				if !errors.Is(result, Unauthenticated) || !errors.As(result, &source) || !errors.Is(result, source) {
					t.Fatal("session rejection lost classification or source identity")
				}
				if private != nil && (!errors.Is(result, private) || !errors.Is(result, cause)) {
					t.Fatal("session mutation lost its private cause identity")
				}
				*source = responseError{code: "private replacement", cause: cause}
			}
			for _, format := range []string{"%v", "%+v", "%#v", "%s"} {
				if got := fmt.Sprintf(format, result); got != want {
					t.Fatalf("session stage diagnostic = %q, want %q", got, want)
				}
			}
			if cause.calls.Load() != 0 {
				t.Fatal("session stage formatted an application error")
			}
		})
	}
}
