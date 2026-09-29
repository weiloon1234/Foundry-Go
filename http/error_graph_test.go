package http

import (
	"context"
	"errors"
	"net/http/httptest"
	"sync/atomic"
	"testing"
	"time"

	"github.com/weiloon1234/Foundry-Go/auth"
	"github.com/weiloon1234/Foundry-Go/auth/lockout"
	"github.com/weiloon1234/Foundry-Go/fault"
	"github.com/weiloon1234/Foundry-Go/idempotency"
)

type cyclicHTTPError struct{ visits atomic.Int32 }

func (*cyclicHTTPError) Error() string { panic("private HTTP error must not be formatted") }
func (e *cyclicHTTPError) Unwrap() error {
	// Finite escape keeps a regression from hanging the whole verification run.
	if e.visits.Add(1) > 4096 {
		return nil
	}
	return e
}

func TestCyclicHTTPFailuresFinishAndPermitTheNextRequest(t *testing.T) {
	for _, site := range []string{"preparation", "authorization", "handler"} {
		t.Run(site, func(t *testing.T) {
			cycle := new(cyclicHTTPError)
			fail := true
			failure := func() error {
				if fail {
					return cycle
				}
				return nil
			}
			endpoint := errorEndpoint("errors.cyclic", "/cyclic")
			handler := returnHTTPError(nil)
			switch site {
			case "preparation":
				endpoint = endpoint.WithPreparation(func(context.Context, Input[NoPath, NoQuery, NoBody]) (NoQuery, NoBody, error) {
					return NoQuery{}, NoBody{}, failure()
				})
			case "authorization":
				endpoint = endpoint.WithAuthorization(func(context.Context, Input[NoPath, NoQuery, NoBody]) error { return failure() })
			case "handler":
				handler = func(context.Context, Input[NoPath, NoQuery, NoBody]) (NoContent, error) {
					return NoContent{}, failure()
				}
			}
			router, err := NewRouter(endpoint.Handle(handler))
			if err != nil {
				t.Fatal(err)
			}
			response := httptest.NewRecorder()
			router.ServeHTTP(response, httptest.NewRequest("GET", "/cyclic", nil))
			if response.Code != 500 || decodeFailure(t, response).Code != InternalError {
				t.Fatal("cyclic failure did not become a safe internal response")
			}
			if n := cycle.visits.Load(); n == 0 || n > 256 {
				t.Fatal("classification exceeded its traversal budget", n)
			}
			fail = false
			response = httptest.NewRecorder()
			router.ServeHTTP(response, httptest.NewRequest("GET", "/cyclic", nil))
			if response.Code != 204 {
				t.Fatal("a completed failure prevented the next request")
			}
		})
	}
}

func TestHTTPClassificationAndRetrySearchesBoundCyclicCauses(t *testing.T) {
	for _, test := range []struct {
		name    string
		failure func(error) error
		code    ErrorCode
		retry   string
	}{
		{"explicit", BadRequest.WithCause, BadRequest, ""},
		{"rate limit without retry", RateLimited.WithCause, RateLimited, ""},
		{"idempotency without retry", IdempotencyInProgress.WithCause, IdempotencyInProgress.definition.Code, ""},
		{"idempotency with retry", func(cause error) error {
			return &idempotencyRetryError{IdempotencyCapacity, cause, 1500 * time.Millisecond}
		}, IdempotencyCapacity.definition.Code, "2"},
	} {
		t.Run(test.name, func(t *testing.T) {
			cycle := new(cyclicHTTPError)
			response := httptest.NewRecorder()
			if err := WriteError(response, httptest.NewRequest("POST", "/", nil), test.failure(cycle)); err != nil {
				t.Fatal(err)
			}
			if decodeFailure(t, response).Code != test.code || response.Header().Get("Retry-After") != test.retry {
				t.Fatal("explicit classification or retry metadata changed")
			}
			if n := cycle.visits.Load(); n > 256 {
				t.Fatal("retry search exceeded its traversal budget", n)
			}
		})
	}
}

func TestAuthenticationErrorGraphRetainsPriority(t *testing.T) {
	for _, test := range []struct {
		err  error
		code ErrorCode
	}{
		{errors.Join(auth.Forbidden, lockout.Locked), RateLimited},
		{errors.Join(auth.Forbidden, lockout.Unavailable), Unavailable},
		{errors.Join(lockout.Unavailable, context.DeadlineExceeded), Unavailable},
		{errors.Join(auth.Forbidden, fault.Overloaded), Unavailable},
		{fault.Wrap(fault.Overloaded, "capacity", context.DeadlineExceeded), Unavailable},
		{errors.Join(auth.Forbidden, auth.Unauthenticated), Unauthenticated},
		{errors.Join(auth.MFARequired, auth.Forbidden), Forbidden},
		{errors.Join(context.Canceled, auth.MFARequired), MFARequired},
		{context.Canceled, Unavailable},
	} {
		if code, found := authenticationCode(test.err); !found || code != test.code {
			t.Fatal("authentication precedence changed", code)
		}
	}
	cycle := new(cyclicHTTPError)
	if _, found := authenticationCode(errors.Join(auth.Forbidden, cycle)); found || cycle.visits.Load() > 256 {
		t.Fatal("incomplete graph established an authentication classification")
	}
	cycle = new(cyclicHTTPError)
	payload, err := errorResponse(t.Context(), authenticationError(cycle))
	if err != nil || payload.Code != InternalError || cycle.visits.Load() > 256 {
		t.Fatal("authentication error did not become a bounded internal failure")
	}
}

func TestIdempotencyHTTPMappingBoundsUnknownCauses(t *testing.T) {
	for _, known := range []bool{false, true} {
		cycle := new(cyclicHTTPError)
		var input error = cycle
		want := InternalError
		if known {
			input = errors.Join(idempotency.Unavailable, cycle)
			want = IdempotencyUnavailable.definition.Code
		}
		mapped := idempotencyHTTPError(input, time.Second)
		response := httptest.NewRecorder()
		if err := WriteError(response, httptest.NewRequest("POST", "/", nil), mapped); err != nil {
			t.Fatal(err)
		}
		if decodeFailure(t, response).Code != want || cycle.visits.Load() > 6*256 {
			t.Fatal("idempotency error mapping lost bounded classification")
		}
		if known && response.Header().Get("Retry-After") != "1" {
			t.Fatal("known idempotency retry was lost")
		}
	}
}
