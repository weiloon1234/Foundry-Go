package httpendpoints_test

import (
	"context"
	"encoding/json"
	"io"
	"net/http"
	"net/http/httptest"
	"strings"
	"sync/atomic"
	"testing"
	"time"

	"foundry.test/consumer/httpendpoints"
	"github.com/weiloon1234/Foundry-Go/fault"
	foundryhttp "github.com/weiloon1234/Foundry-Go/http"
)

type retryMethodPanic struct{}

func (retryMethodPanic) Error() string { return "private-retry-marker" }
func (retryMethodPanic) Is(error) bool { panic("private-retry-marker") }

type retryUnwrapPanic struct{}

func (retryUnwrapPanic) Error() string { return "private-retry-marker" }
func (retryUnwrapPanic) Unwrap() error { panic("private-retry-marker") }

type reservationRecovers struct {
	err   error
	calls atomic.Int32
}

func (s *reservationRecovers) Reserve(context.Context) error {
	if s.calls.Add(1) == 1 {
		return s.err
	}
	return nil
}

func TestUnavailableRetryClassificationPreservesHTTPAndNextRequest(t *testing.T) {
	t.Parallel()
	for _, test := range []struct {
		name  string
		cause error
		retry string
	}{
		{"panicking Is", retryMethodPanic{}, ""},
		{"panicking Unwrap", retryUnwrapPanic{}, ""},
		{"native overload", fault.New(fault.Overloaded, "private-retry-marker"), "1"},
	} {
		t.Run(test.name, func(t *testing.T) {
			service := &reservationRecovers{err: foundryhttp.Unavailable.WithCause(test.cause)}
			router, err := httpendpoints.ReservationRouter(service)
			if err != nil {
				t.Fatal(err)
			}
			server := httptest.NewServer(router)
			t.Cleanup(server.Close)
			client := server.Client()
			client.Timeout = 3 * time.Second
			for attempt := range 2 {
				request, err := http.NewRequestWithContext(t.Context(), "POST", server.URL+"/seats/reserve", nil)
				if err != nil {
					t.Fatal(err)
				}
				response, err := client.Do(request)
				if err != nil {
					t.Fatal("request aborted during error classification", err)
				}
				body, readErr := io.ReadAll(io.LimitReader(response.Body, 1<<16))
				response.Body.Close()
				if readErr != nil {
					t.Fatal(readErr)
				}
				if attempt == 1 {
					if response.StatusCode != http.StatusNoContent || len(body) != 0 {
						t.Fatal("healthy request failed after classification failure", response.StatusCode)
					}
					continue
				}
				var payload foundryhttp.ErrorResponse
				if err := json.Unmarshal(body, &payload); err != nil {
					t.Fatal(err)
				}
				if response.StatusCode != http.StatusServiceUnavailable || payload.Status != response.StatusCode || payload.Code != foundryhttp.Unavailable || response.Header.Get("Retry-After") != test.retry || strings.Contains(string(body), "private-retry-marker") {
					t.Fatal("unavailable response lost safe status, retry or redaction")
				}
			}
		})
	}
}
