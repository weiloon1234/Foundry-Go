package http

import (
	"bytes"
	"encoding/json"
	"log/slog"
	stdhttp "net/http"
	"net/http/httptest"
	"strings"
	"testing"
)

func TestPathFailureKeepsInjectedLoggerAndCorrelation(t *testing.T) {
	var output bytes.Buffer
	logger := slog.New(slog.NewJSONHandler(&output, nil)).With("application", "consumer")
	route := DefineRoute(RouteSpec{ID: "members.show", Method: GET, Access: Public}, DefinePath("/members/{text}",
		Param("text", StringPath[string](), func(*textPath) *string { panic("private-selector-payload") }),
	))
	router, err := NewRouter(route.HandleRaw(func(stdhttp.ResponseWriter, *stdhttp.Request, textPath) { t.Error("broken selector reached handler") }))
	if err != nil {
		t.Fatal(err)
	}
	handler := newHandlerLifetime().wrap(router, logger, DefaultServerConfig())
	recorder := httptest.NewRecorder()
	handler.ServeHTTP(recorder, httptest.NewRequest("GET", "/members/private-path-value", nil))
	failure := decodeFailure(t, recorder)
	if failure.Code != InternalError || failure.RequestID == "" {
		t.Fatalf("path failure: %+v", failure)
	}
	var entry struct {
		Application string `json:"application"`
		Message     string `json:"msg"`
		Route       string `json:"route_id"`
		Request     string `json:"request_id"`
		Error       string `json:"error"`
	}
	if err := json.Unmarshal(output.Bytes(), &entry); err != nil {
		t.Fatal(err)
	}
	if entry.Application != "consumer" || entry.Route != "members.show" || entry.Request != string(failure.RequestID) || entry.Message != "HTTP path decoding failed" || !strings.Contains(entry.Error, "panicked") {
		t.Fatalf("diagnostic lost context: %+v", entry)
	}
	if strings.Contains(output.String(), "private-selector-payload") || strings.Contains(output.String(), "private-path-value") {
		t.Fatal("route diagnostic exposed a panic payload or path value")
	}
}
