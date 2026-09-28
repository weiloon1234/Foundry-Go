package httpendpoints_test

import (
	"context"
	"encoding/json"
	"errors"
	"io"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"foundry.test/consumer/httpendpoints"
	foundryhttp "github.com/weiloon1234/Foundry-Go/http"
)

type reservationFailure struct{ err error }

func (s reservationFailure) Reserve(context.Context) error { return s.err }

func TestDeclaredApplicationErrorOverHTTP(t *testing.T) {
	t.Parallel()
	expected, err := httpendpoints.SeatUnavailable.Description()
	if err != nil {
		t.Fatal(err)
	}
	cause := errors.New("private-database-marker")
	returned := httpendpoints.SeatUnavailable.WithCause(cause)
	if !errors.Is(returned, cause) || !errors.Is(returned, httpendpoints.SeatUnavailable) {
		t.Fatal("ordinary Go error identity lost")
	}
	for _, tc := range []struct {
		err    error
		code   foundryhttp.ErrorCode
		status int
	}{
		{returned, expected.Code, expected.Status},
		{foundryhttp.DefineError("seats.unknown", 409, "Undeclared"), foundryhttp.InternalError, 500},
		{foundryhttp.Forbidden, foundryhttp.Forbidden, 403},
	} {
		router, err := httpendpoints.ReservationRouter(reservationFailure{tc.err})
		if err != nil {
			t.Fatal(err)
		}
		server := httptest.NewServer(router)
		client := server.Client()
		client.Timeout = 3 * time.Second
		request, err := http.NewRequestWithContext(t.Context(), "POST", server.URL+"/seats/reserve", nil)
		if err != nil {
			server.Close()
			t.Fatal(err)
		}
		response, err := client.Do(request)
		if err != nil {
			server.Close()
			t.Fatal(err)
		}
		body, err := io.ReadAll(io.LimitReader(response.Body, 1<<16))
		response.Body.Close()
		server.Close()
		if err != nil {
			t.Fatal(err)
		}
		var payload foundryhttp.ErrorResponse
		if err := json.Unmarshal(body, &payload); err != nil {
			t.Fatal(err)
		}
		if response.StatusCode != tc.status || payload.Code != tc.code || payload.Status != tc.status || strings.Contains(string(body), "private-database-marker") {
			t.Fatalf("unsafe error: %s", body)
		}
		if tc.code == expected.Code && payload.Message != expected.Message {
			t.Fatal("public message differs from declaration")
		}
		endpoints := router.Endpoints()
		if len(endpoints) != 1 || len(endpoints[0].Errors) != 1 || endpoints[0].Errors[0] != expected {
			t.Fatal("transport contract lost application error")
		}
		found := false
		for _, definition := range router.ErrorDefinitions() {
			if definition == expected {
				found = true
			}
		}
		if !found {
			t.Fatal("router contract omitted custom error")
		}
	}
}
