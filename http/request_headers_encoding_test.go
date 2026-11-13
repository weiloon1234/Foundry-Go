package http_test

import (
	"context"
	"errors"
	"net/http/httptest"
	"testing"

	"github.com/weiloon1234/Foundry-Go/fault"
	foundryhttp "github.com/weiloon1234/Foundry-Go/http"
	httptestkit "github.com/weiloon1234/Foundry-Go/testkit/http"
	"github.com/weiloon1234/Foundry-Go/value"
)

func TestRequestHeaderEncodeRealOutboundRoundTrip(t *testing.T) {
	seen := make(chan headerParameters, 1)
	router, err := foundryhttp.NewRouter(headerEndpoint(mixedHeaderParameters()).Handle(func(_ context.Context, in foundryhttp.Input[foundryhttp.NoPath, headerParameters, foundryhttp.NoBody]) (foundryhttp.NoContent, error) {
		seen <- in.Query
		return foundryhttp.NoContent{}, nil
	}))
	if err != nil {
		t.Fatal(err)
	}
	client := httptestkit.New(t, router)
	input := headerParameters{Query: "query + value", Version: "1,2+%20", Device: value.Set("")}
	headers, err := foundryhttp.EncodeRequestHeaders(mixedHeaderParameters(), t.Context(), input, foundryhttp.DefaultEndpointLimits().Headers)
	if err != nil {
		t.Fatal(err)
	}
	if headers.Get("X-App-Version") != input.Version || len(headers.Values("X-Device-ID")) != 1 {
		t.Fatal("header encoding changed text or presence")
	}
	location, err := headerEndpoint(mixedHeaderParameters()).URL(t.Context(), foundryhttp.NoPath{}, input)
	if err != nil {
		t.Fatal(err)
	}
	request := client.Get(location)
	for name, lines := range headers {
		for _, line := range lines {
			request = request.Header(name, line)
		}
	}
	response, err := client.Do(t.Context(), request)
	if err != nil || response.Status() != 204 {
		t.Fatalf("outbound round trip: %v, %d", err, response.Status())
	}
	if got := <-seen; got != input {
		t.Fatalf("got %+v want %+v", got, input)
	}
	headers.Set("X-App-Version", "changed")
	again, err := foundryhttp.EncodeRequestHeaders(mixedHeaderParameters(), t.Context(), input, foundryhttp.DefaultEndpointLimits().Headers)
	if err != nil || again.Get("X-App-Version") != input.Version {
		t.Fatal("encoder shared its output")
	}
}

func TestRequestHeaderFramingPrecedesCodecsAndContainsFailures(t *testing.T) {
	calls := 0
	codec := &queryFuncCodec{parse: func(text string) (string, error) { calls++; return text, nil }}
	d := foundryhttp.DefineQuery(foundryhttp.HeaderParam("X-Text", codec, func(q *queryCustom) *string { return &q.Text }))
	route := foundryhttp.DefineRoute(foundryhttp.RouteSpec{ID: "headers.callbacks", Method: foundryhttp.GET, Access: foundryhttp.Public}, foundryhttp.StaticPath("/headers"))
	e := foundryhttp.DefineEndpoint(route, d, foundryhttp.EmptyBody(), foundryhttp.EmptyResponse(204))
	router, err := foundryhttp.NewRouter(e.Handle(func(context.Context, foundryhttp.Input[foundryhttp.NoPath, queryCustom, foundryhttp.NoBody]) (foundryhttp.NoContent, error) {
		return foundryhttp.NoContent{}, nil
	}))
	if err != nil {
		t.Fatal(err)
	}
	for _, lines := range [][]string{nil, {"a", "b"}, {"private\nvalue"}} {
		r := httptest.NewRequest("GET", "/headers", nil)
		r.Header["X-Text"] = lines
		w := httptest.NewRecorder()
		router.ServeHTTP(w, r)
		if w.Code != 400 || calls != 0 {
			t.Fatal("codec ran before framing validation", w.Code, calls)
		}
	}
	codec.parse = func(string) (string, error) { panic("private header panic") }
	r := httptest.NewRequest("GET", "/headers", nil)
	r.Header.Set("X-Text", "ok")
	w := httptest.NewRecorder()
	router.ServeHTTP(w, r)
	if w.Code != 500 {
		t.Fatal("callback failure attributed to input", w.Code)
	}
	codec.format = func(string) (string, error) { panic("private formatter panic") }
	if out, err := foundryhttp.EncodeRequestHeaders(d, t.Context(), queryCustom{}, foundryhttp.DefaultEndpointLimits().Headers); out != nil || !errors.Is(err, fault.Internal) {
		t.Fatal("formatter panic escaped", err)
	}
}

func TestRequestHeaderEncodeRejectsUnsafeOrExcessiveOutput(t *testing.T) {
	for _, text := range []string{" lead", "trail\t", "é", "private\nvalue"} {
		headers, err := foundryhttp.EncodeRequestHeaders(mixedHeaderParameters(), t.Context(), headerParameters{Version: text}, foundryhttp.DefaultEndpointLimits().Headers)
		var failure *foundryhttp.RequestHeaderError
		if headers != nil || !errors.As(err, &failure) {
			t.Fatalf("accepted unsafe output %q: %v", text, err)
		}
		if failure.Issues()[0].Path != "/X-App-Version" {
			t.Fatal("wrong encoded issue path")
		}
	}
	limits := foundryhttp.DefaultEndpointLimits().Headers
	limits.FieldBytes = 2
	if out, err := foundryhttp.EncodeRequestHeaders(mixedHeaderParameters(), t.Context(), headerParameters{Version: "123"}, limits); err == nil || out != nil {
		t.Fatal("field limit bypassed")
	}
	limits.FieldBytes, limits.Bytes = 27, 27
	if out, err := foundryhttp.EncodeRequestHeaders(mixedHeaderParameters(), t.Context(), headerParameters{Version: "123", Device: value.Set("123")}, limits); err == nil || out != nil {
		t.Fatal("aggregate limit bypassed")
	}
	ctx, cancel := context.WithCancel(t.Context())
	cancel()
	if _, err := foundryhttp.EncodeRequestHeaders(mixedHeaderParameters(), ctx, headerParameters{}, limits); !errors.Is(err, context.Canceled) {
		t.Fatal("cancellation lost", err)
	}
	if _, err := foundryhttp.EncodeRequestHeaders(mixedHeaderParameters(), nil, headerParameters{}, limits); err == nil {
		t.Fatal("nil context accepted")
	}
	empty, err := foundryhttp.EncodeRequestHeaders(foundryhttp.EmptyQuery(), t.Context(), foundryhttp.NoQuery{}, foundryhttp.RequestHeaderLimits{})
	if err != nil || empty == nil || len(empty) != 0 {
		t.Fatal("header-free encoder failed", err)
	}
}
