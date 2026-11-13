package httpquery_test

import (
	"context"
	"encoding/json"
	stdhttp "net/http"
	"net/http/httptest"
	"reflect"
	"strings"
	"testing"

	"foundry.test/consumer/httpquery"
	foundryhttp "github.com/weiloon1234/Foundry-Go/http"
	"github.com/weiloon1234/Foundry-Go/value"
)

func TestConsumerRequestHeaderDeclarations(t *testing.T) {
	parameters := httpquery.RequestParametersDescriptor()
	headers, err := foundryhttp.RequestHeaderParameters(parameters)
	if err != nil {
		t.Fatal(err)
	}
	if len(headers) != 2 || headers[0].Name != "X-App-Version" || !headers[0].Required ||
		headers[1].Name != "X-Device-Id" || headers[1].Required || headers[0].Repeated || headers[1].Repeated {
		t.Fatalf("required and optional header declarations: %+v", headers)
	}
	for _, header := range headers {
		if header.Scalar == nil || header.Scalar.Syntax != foundryhttp.TextURLSyntax {
			t.Fatalf("header scalar metadata missing: %+v", header)
		}
	}
	query, err := foundryhttp.QueryParameters(parameters)
	if err != nil || len(query) != 1 || query[0].Name != "q" || query[0].Required {
		t.Fatalf("query projection: %+v, %v", query, err)
	}
	info, err := foundryhttp.InspectEndpoint(httpquery.RequestHeadersEndpoint())
	if err != nil {
		t.Fatal(err)
	}
	if !reflect.DeepEqual(info.Headers, headers) || !reflect.DeepEqual(info.Query, query) {
		t.Fatalf("endpoint metadata differs from generated declarations: %+v", info)
	}
	headers[0].Name = "X-Changed"
	info.Headers[1].Name = "X-Changed"
	again, err := foundryhttp.RequestHeaderParameters(httpquery.RequestParametersDescriptor())
	if err != nil || again[0].Name != "X-App-Version" || again[1].Name != "X-Device-Id" {
		t.Fatalf("inspection changed the cached descriptor: %+v, %v", again, err)
	}
}

func TestConsumerRequestHeadersRoundTrip(t *testing.T) {
	called := false
	router, err := foundryhttp.NewRouter(httpquery.RequestHeadersEndpoint().Handle(func(_ context.Context, in foundryhttp.Input[foundryhttp.NoPath, httpquery.RequestParameters, foundryhttp.NoBody]) (httpquery.RequestHeadersResponse, error) {
		called = true
		return httpquery.RequestHeadersResponse{Search: in.Query.Search, AppVersion: in.Query.AppVersion, DeviceID: in.Query.DeviceID}, nil
	}))
	if err != nil {
		t.Fatal(err)
	}
	serve := func(t *testing.T, headers stdhttp.Header) *httptest.ResponseRecorder {
		t.Helper()
		called = false
		request := httptest.NewRequest("GET", "/headers?q=search", nil)
		request.Header = headers
		response := httptest.NewRecorder()
		router.ServeHTTP(response, request)
		vary := "," + strings.ToLower(strings.ReplaceAll(strings.Join(response.Header().Values("Vary"), ","), " ", "")) + ","
		for _, name := range []string{"x-app-version", "x-device-id"} {
			if !strings.Contains(vary, ","+name+",") {
				t.Fatalf("Vary missing %s: %s", name, vary)
			}
		}
		return response
	}
	assertEcho := func(t *testing.T, response *httptest.ResponseRecorder, want httpquery.RequestParameters) {
		t.Helper()
		var got httpquery.RequestHeadersResponse
		if err := json.Unmarshal(response.Body.Bytes(), &got); err != nil {
			t.Fatal(err)
		}
		if response.Code != 200 || !called || got.Search != want.Search || got.AppVersion != want.AppVersion || got.DeviceID != want.DeviceID {
			t.Fatalf("echo: status=%d called=%t got=%+v want=%+v", response.Code, called, got, want)
		}
	}
	for _, tc := range []struct {
		name    string
		headers stdhttp.Header
		device  value.Optional[string]
	}{
		{"both", stdhttp.Header{"x-aPP-version": {"1.2"}, "X-dEVICE-id": {"device+%20"}}, value.Set("device+%20")},
		{"omitted", stdhttp.Header{"X-App-Version": {"1.2"}}, value.Optional[string]{}},
		{"empty", stdhttp.Header{"X-App-Version": {"1.2"}, "X-Device-ID": {""}}, value.Set("")},
	} {
		t.Run(tc.name, func(t *testing.T) {
			want := httpquery.RequestParameters{Search: value.Set("search"), AppVersion: "1.2", DeviceID: tc.device}
			assertEcho(t, serve(t, tc.headers), want)
			encoded, err := foundryhttp.EncodeRequestHeaders(httpquery.RequestParametersDescriptor(), t.Context(), want, foundryhttp.DefaultEndpointLimits().Headers)
			if err != nil {
				t.Fatal(err)
			}
			assertEcho(t, serve(t, encoded), want)
		})
	}
	response := serve(t, stdhttp.Header{"X-Device-ID": {"device"}})
	var failure foundryhttp.ErrorResponse
	if err := json.Unmarshal(response.Body.Bytes(), &failure); err != nil {
		t.Fatal(err)
	}
	if response.Code != 400 || called || len(failure.Issues) != 1 || failure.Issues[0].Path != "/headers/X-App-Version" {
		t.Fatalf("missing required header: status=%d called=%t failure=%+v", response.Code, called, failure)
	}
}
