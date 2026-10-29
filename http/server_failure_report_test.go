package http

import (
	"bytes"
	"context"
	"errors"
	"log/slog"
	stdhttp "net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/weiloon1234/Foundry-Go/database"
	"github.com/weiloon1234/Foundry-Go/fault"
	"github.com/weiloon1234/Foundry-Go/internal/safeerror"
)

type privateCause struct{}

func (privateCause) Error() string { return "password=hunter2" }

func TestServerFailuresAreReportedWithRedactedDiagnostics(t *testing.T) {
	var output bytes.Buffer
	logger := slog.New(slog.NewJSONHandler(&output, nil))
	route := DefineRoute(RouteSpec{ID: "members.fail", Method: GET, Access: Public}, DefinePath[NoPath]("/fail"))
	router, err := NewRouter(HandleRaw(route, func(w stdhttp.ResponseWriter, r *stdhttp.Request, _ NoPath) {
		_ = WriteError(w, r, errors.Join(safeerror.WrapFault(fault.Internal, "database lookup failed", privateCause{})))
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

func TestApplicationFaultMessagesAreNotLogDiagnosticAuthority(t *testing.T) {
	var output bytes.Buffer
	logger := slog.New(slog.NewJSONHandler(&output, nil))
	route := DefineRoute(RouteSpec{ID: "members.private", Method: GET, Access: Public}, DefinePath[NoPath]("/private"))
	router, err := NewRouter(HandleRaw(route, func(w stdhttp.ResponseWriter, r *stdhttp.Request, _ NoPath) {
		_ = WriteError(w, r, fault.Wrap(fault.Internal, "password=hunter2", privateCause{}))
	}))
	if err != nil {
		t.Fatal(err)
	}
	recorder := httptest.NewRecorder()
	newHandlerLifetime().wrap(router, logger, DefaultServerConfig()).ServeHTTP(recorder, httptest.NewRequest("GET", "/private", nil))
	if recorder.Code != 500 || !strings.Contains(output.String(), "HTTP request failed") || strings.Contains(output.String(), "hunter2") {
		t.Fatal("application fault text entered failure report")
	}
}

func TestAssetAdmissionAndPrivateCodesReporting(t *testing.T) {
	closed := &Assets{closing: true}
	_, refused := closed.acquire(context.Background())
	for _, test := range []struct {
		name   string
		err    error
		report bool
	}{
		{"draining assets", refused, false},
		{"unavailable failure", Unavailable.WithCause(privateCause{}), true},
		{"explicit unavailable cause", Unavailable.WithCause(Unavailable), true},
		{"private transport code", ErrorCode("password=hunter2").WithCause(nil), true},
		{"private database code", database.NewError("password=hunter2", database.Code("password=hunter2")), true},
	} {
		t.Run(test.name, func(t *testing.T) {
			var output bytes.Buffer
			logger := slog.New(slog.NewJSONHandler(&output, nil))
			route := DefineRoute(RouteSpec{ID: "report.test", Method: GET, Access: Public}, DefinePath[NoPath]("/"))
			router, err := NewRouter(HandleRaw(route, func(w stdhttp.ResponseWriter, r *stdhttp.Request, _ NoPath) { _ = WriteError(w, r, test.err) }))
			if err != nil {
				t.Fatal(err)
			}
			recorder := httptest.NewRecorder()
			newHandlerLifetime().wrap(router, logger, DefaultServerConfig()).ServeHTTP(recorder, httptest.NewRequest("GET", "/", nil))
			if strings.Contains(output.String(), "HTTP request failed") != test.report || strings.Contains(output.String(), "hunter2") {
				t.Fatal("incorrect failure reporting", output.String())
			}
			if test.name == "draining assets" && recorder.Code != 503 {
				t.Fatal(recorder.Code)
			}
		})
	}
}

func TestHTTPBareDecisionRetainsEnumIdentity(t *testing.T) {
	var code ErrorCode
	if !errors.As(NotFound.WithCause(nil), &code) || code != NotFound {
		t.Fatal("HTTP enum identity lost")
	}
}
