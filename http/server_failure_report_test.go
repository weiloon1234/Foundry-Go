package http

import (
	"bytes"
	"errors"
	"log/slog"
	stdhttp "net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/weiloon1234/Foundry-Go/fault"
)

type privateCause struct{}

func (privateCause) Error() string { return "password=hunter2" }

func TestServerFailuresAreReportedWithRedactedDiagnostics(t *testing.T) {
	var output bytes.Buffer
	logger := slog.New(slog.NewJSONHandler(&output, nil))
	route := DefineRoute(RouteSpec{ID: "members.fail", Method: GET, Access: Public}, DefinePath[NoPath]("/fail"))
	router, err := NewRouter(route.HandleRaw(func(w stdhttp.ResponseWriter, r *stdhttp.Request, _ NoPath) {
		_ = WriteError(w, r, errors.Join(fault.Wrap(fault.Internal, "database lookup failed", privateCause{})))
	}))
	if err != nil {
		t.Fatal(err)
	}
	handler := newHandlerLifetime().wrap(router, logger, DefaultServerConfig())
	recorder := httptest.NewRecorder()
	handler.ServeHTTP(recorder, httptest.NewRequest("GET", "/fail", nil))
	if recorder.Code != 500 {
		t.Fatal(recorder.Code)
	}
	logged := output.String()
	if !strings.Contains(logged, "HTTP request failed") || !strings.Contains(logged, "database lookup failed") || !strings.Contains(logged, "members.fail") {
		t.Fatal("server failure was not reported", logged)
	}
	if strings.Contains(logged, "hunter2") {
		t.Fatal("server failure report formatted an application error", logged)
	}
}

func TestOverloadIsRetryableUnavailable(t *testing.T) {
	for _, failure := range []error{fault.New(fault.Overloaded, "capacity"), Unavailable.WithCause(fault.New(fault.Overloaded, "capacity"))} {
		recorder := httptest.NewRecorder()
		if err := WriteError(recorder, httptest.NewRequest("GET", "/", nil), failure); err != nil {
			t.Fatal(err)
		}
		if recorder.Code != 503 || recorder.Header().Get("Retry-After") != "1" || decodeFailure(t, recorder).Code != Unavailable {
			t.Fatal("overload was not a retryable 503", recorder.Code, recorder.Header())
		}
	}
}
