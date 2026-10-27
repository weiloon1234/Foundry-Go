package http

import (
	"context"
	"errors"
	"github.com/weiloon1234/Foundry-Go/internal/errorgraphtest"
	"io"
	"net/http/httptest"
	"testing"
)

func TestRawRequestReaderContainsErrorInspection(t *testing.T) {
	for _, mode := range []string{"cycle", "panic", "goexit"} {
		t.Run(mode, func(t *testing.T) {
			cause := &errorgraphtest.Error{Mode: mode}
			var retained error
			router, err := NewRouter(rawEndpoint(16).Handle(func(_ context.Context, in Input[NoPath, NoQuery, RawBody]) (EndpointReply, error) {
				_, retained = io.ReadAll(in.Body)
				return EndpointReply{}, retained
			}))
			if err != nil {
				t.Fatal(err)
			}
			req := httptest.NewRequest("POST", "/raw", cause)
			req.Header.Set("Content-Type", "text/plain")
			response := httptest.NewRecorder()
			router.ServeHTTP(response, req)
			expected := httptest.NewRecorder()
			writeRoutingError(expected, httptest.NewRequest("POST", "/raw", nil), InternalError.WithCause(nil))
			if response.Code != 500 || response.Body.String() != expected.Body.String() || !errors.Is(retained, cause) || cause.Formatted.Load() != 0 || cause.Visits.Load() > 512 {
				t.Fatal("failed body inspection changed HTTP bytes, lost identity, or escaped containment", response.Code, response.Body.String())
			}
		})
	}
}
