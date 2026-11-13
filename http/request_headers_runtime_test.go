package http

import (
	"context"
	"errors"
	stdhttp "net/http"
	"net/http/httptest"
	"reflect"
	"slices"
	"strings"
	"testing"
	"time"

	"github.com/weiloon1234/Foundry-Go/contract"
	"github.com/weiloon1234/Foundry-Go/fault"
	"github.com/weiloon1234/Foundry-Go/testkit"
	"github.com/weiloon1234/Foundry-Go/validation"
	"github.com/weiloon1234/Foundry-Go/value"
)

type runtimeHeaderParams struct {
	Query    string
	Revision int
	Note     value.Optional[string]
	Enabled  value.Optional[bool]
}

type runtimeHeaderInput = Input[NoPath, runtimeHeaderParams, NoBody]

func runtimeHeaders() Query[runtimeHeaderParams] {
	return DefineQuery(
		QueryParam("X-Revision", StringQuery[string](), func(q *runtimeHeaderParams) *string { return &q.Query }),
		HeaderParam("x-revision", IntegerQuery[int](), func(q *runtimeHeaderParams) *int { return &q.Revision }),
		OptionalHeaderParam("x-note", StringQuery[string](), func(q *runtimeHeaderParams) *value.Optional[string] { return &q.Note }),
		OptionalHeaderParam("x-enabled", BoolQuery[bool](), func(q *runtimeHeaderParams) *value.Optional[bool] { return &q.Enabled }),
	)
}

func runtimeHeaderEndpoint(options ...EndpointOption) Endpoint[NoPath, runtimeHeaderParams, NoBody, NoContent] {
	route := DefineRoute(RouteSpec{ID: "runtime.headers", Method: GET, Access: Public}, StaticPath("/headers"))
	return DefineEndpoint(route, runtimeHeaders(), EmptyBody(), EmptyResponse(204), options...)
}

func headerRuntimeCall(handler stdhttp.Handler, headers stdhttp.Header) *httptest.ResponseRecorder {
	r := httptest.NewRequest("GET", "/headers?X-Revision=query", nil)
	r.Header = headers
	w := httptest.NewRecorder()
	handler.ServeHTTP(w, r)
	return w
}

func assertHeaderVary(t *testing.T, header stdhttp.Header, names ...string) {
	t.Helper()
	var got []string
	for _, line := range header.Values("Vary") {
		for _, name := range strings.Split(line, ",") {
			got = append(got, strings.ToLower(strings.TrimSpace(name)))
		}
	}
	for _, name := range names {
		if !slices.Contains(got, "*") && !slices.Contains(got, strings.ToLower(name)) {
			t.Fatalf("missing Vary %s: %v", name, got)
		}
	}
}

func TestRequestHeaderRuntimePresenceAndSources(t *testing.T) {
	var got runtimeHeaderParams
	router, err := NewRouter(runtimeHeaderEndpoint().Handle(func(_ context.Context, in runtimeHeaderInput) (NoContent, error) {
		got = in.Query
		return NoContent{}, nil
	}))
	if err != nil {
		t.Fatal(err)
	}
	for _, headers := range []stdhttp.Header{
		{"x-revision": {"0"}},
		{"X-Revision": {" \t12\t "}, "X-Note": {""}, "X-Enabled": {"false"}},
		{"X-Revision": {"12"}, "X-Note": {" a,b+%20\tinside "}, "Unrelated": {"ignored"}},
	} {
		w := headerRuntimeCall(router, headers)
		if w.Code != 204 || got.Query != "query" {
			t.Fatalf("%d: %+v", w.Code, got)
		}
		if headers.Get("X-Note") == "" && len(headers["X-Note"]) == 1 {
			if text, present := got.Note.Get(); !present || text != "" {
				t.Fatal("supplied empty was lost")
			}
		}
		if len(headers["X-Note"]) == 0 && got.Note.IsSet() {
			t.Fatal("omitted header set")
		}
		if headers.Get("X-Enabled") == "false" {
			if enabled, present := got.Enabled.Get(); !present || enabled {
				t.Fatal("supplied false lost")
			}
		}
		if headers.Get("X-Note") != "" {
			if text, _ := got.Note.Get(); text != "a,b+%20\tinside" {
				t.Fatal("header text was URL-decoded or split", text)
			}
		}
		assertHeaderVary(t, w.Header(), "X-Revision", "X-Note", "X-Enabled")
	}
}

func TestRequestHeaderRuntimeRejectsMalformedAndOversized(t *testing.T) {
	for _, tc := range []struct {
		name    string
		headers stdhttp.Header
		path    string
		code    contract.IssueCode
		limits  func(*RequestHeaderLimits)
	}{
		{"missing", nil, "/headers/X-Revision", contract.RequiredIssue, nil},
		{"empty integer", stdhttp.Header{"X-Revision": {""}}, "/headers/X-Revision", contract.ValueIssue, nil},
		{"invalid integer", stdhttp.Header{"X-Revision": {"private-value"}}, "/headers/X-Revision", contract.ValueIssue, nil},
		{"duplicate", stdhttp.Header{"X-Revision": {"1", "1"}}, "/headers/X-Revision", contract.LengthIssue, nil},
		{"case duplicate", stdhttp.Header{"X-Revision": {"1"}, "x-revision": {"2"}}, "/headers/X-Revision", contract.LengthIssue, nil},
		{"non ASCII", stdhttp.Header{"X-Revision": {"1"}, "X-Note": {"é"}}, "/headers/X-Note", contract.ValueIssue, nil},
		{"control", stdhttp.Header{"X-Revision": {"1"}, "X-Note": {"private\r\nvalue"}}, "/headers/X-Note", contract.ValueIssue, nil},
		{"field bytes", stdhttp.Header{"X-Revision": {"1"}, "X-Note": {"   x "}}, "/headers/X-Note", contract.LengthIssue, func(l *RequestHeaderLimits) { l.FieldBytes = 4 }},
		{"aggregate bytes", stdhttp.Header{"X-Revision": {"1"}, "X-Note": {strings.Repeat("x", 49)}}, "/headers", contract.LengthIssue, func(l *RequestHeaderLimits) { l.FieldBytes = 64; l.Bytes = 64 }},
		{"value count", stdhttp.Header{"X-Revision": {"1", "1", "1", "1"}}, "/headers", contract.LengthIssue, func(l *RequestHeaderLimits) { l.Fields = 3; l.Values = 3 }},
		{"invalid bool", stdhttp.Header{"X-Revision": {"1"}, "X-Enabled": {"0"}}, "/headers/X-Enabled", contract.ValueIssue, nil},
	} {
		t.Run(tc.name, func(t *testing.T) {
			limits := DefaultEndpointLimits()
			if tc.limits != nil {
				tc.limits(&limits.Headers)
			}
			called := false
			router, err := NewRouter(runtimeHeaderEndpoint(Limits(limits)).Handle(func(context.Context, runtimeHeaderInput) (NoContent, error) { called = true; return NoContent{}, nil }))
			if err != nil {
				t.Fatal(err)
			}
			w := headerRuntimeCall(router, tc.headers)
			failure := decodeFailure(t, w)
			if called || w.Code != 400 || len(failure.Issues) != 1 || failure.Issues[0].Path != tc.path || failure.Issues[0].Code != tc.code {
				t.Fatalf("%d: %+v", w.Code, failure)
			}
			if strings.Contains(w.Body.String(), "private") {
				t.Fatal("header value leaked")
			}
			assertHeaderVary(t, w.Header(), "X-Note", "X-Revision")
			if healthy := headerRuntimeCall(router, stdhttp.Header{"X-Revision": {"1"}}); healthy.Code != 204 || !called {
				t.Fatal("healthy request failed after rejection")
			}
		})
	}
}

func TestRequestHeaderRuntimeLifecycleAndValidation(t *testing.T) {
	var stages []string
	revision := validation.DefineField("X-Revision", func(q runtimeHeaderParams) int { return q.Revision })
	note := validation.DefineField("X-Note", func(q runtimeHeaderParams) value.Optional[string] { return q.Note })
	e := ValidateHeaders(runtimeHeaderEndpoint(), validation.Rules(revision, validation.Min(3)), validation.Rules(note, validation.Absent[string]()))
	e = PrepareEndpoint(e, func(_ context.Context, in runtimeHeaderInput) (runtimeHeaderParams, NoBody, error) {
		stages = append(stages, "prepare")
		in.Query.Revision++
		in.Query.Note = value.Optional[string]{}
		return in.Query, in.Body, nil
	})
	e = AuthorizeEndpoint(e, func(_ context.Context, in runtimeHeaderInput) error {
		stages = append(stages, "authorize")
		if in.Query.Revision == 5 {
			return Forbidden
		}
		return nil
	})
	router, err := NewRouter(HandleBound(e, func(_ context.Context, in runtimeHeaderInput) (Handler[NoPath, runtimeHeaderParams, NoBody, NoContent], error) {
		stages = append(stages, "bind")
		return func(_ context.Context, input runtimeHeaderInput) (NoContent, error) {
			stages = append(stages, "handler")
			if input.Query.Query != "query" || input.Query.Revision != 3 {
				t.Error("prepared Q lost inputs")
			}
			return NoContent{}, nil
		}, nil
	}))
	if err != nil {
		t.Fatal(err)
	}
	for _, tc := range []struct {
		headers stdhttp.Header
		status  int
		stages  []string
		path    string
	}{
		{stdhttp.Header{"X-Revision": {"2"}}, 204, []string{"prepare", "authorize", "bind", "handler"}, ""},
		{stdhttp.Header{"X-Revision": {"1"}}, 422, []string{"prepare", "authorize", "bind"}, "/headers/X-Revision"},
		{stdhttp.Header{"X-Revision": {"4"}}, 403, []string{"prepare", "authorize"}, ""},
		{stdhttp.Header{"X-Revision": {"2"}, "X-Note": {""}}, 422, nil, "/headers/X-Note"},
	} {
		stages = nil
		w := headerRuntimeCall(router, tc.headers)
		if w.Code != tc.status || !reflect.DeepEqual(stages, tc.stages) {
			t.Fatalf("%d stages %v; want %d %v", w.Code, stages, tc.status, tc.stages)
		}
		if tc.path != "" && decodeFailure(t, w).Issues[0].Path != tc.path {
			t.Fatal("header validation location lost")
		}
	}
}

func TestRequestHeaderRuntimeDiscardAndSafeFailure(t *testing.T) {
	d := DefineQuery(
		HeaderParam("A", StringQuery[string](), func(q *runtimeHeaderParams) *string { return &q.Query }),
		HeaderParam("Z", IntegerQuery[int](), func(q *runtimeHeaderParams) *int { return &q.Revision }),
	)
	values, err := readRequestHeaders(t.Context(), stdhttp.Header{"A": {"partially decoded"}, "Z": {"bad"}}, d.headerIndexes, d.headerFields, DefaultEndpointLimits().Headers)
	if err != nil {
		t.Fatal(err)
	}
	got, err := decodeRequestHeaders(d, t.Context(), values, runtimeHeaderParams{})
	if err == nil || !reflect.DeepEqual(got, runtimeHeaderParams{}) {
		t.Fatal("partial Q returned")
	}
	var failure *RequestHeaderError
	if !errors.As(err, &failure) || !errors.Is(err, fault.Invalid) || failure.Error() != "invalid request headers" {
		t.Fatal("wrong safe failure", err)
	}
	issues := failure.Issues()
	issues[0].Path = "changed"
	if failure.Issues()[0].Path != "/Z" {
		t.Fatal("issues were shared")
	}
	ctx, cancel := context.WithCancel(t.Context())
	cancel()
	if _, err := readRequestHeaders(ctx, nil, d.headerIndexes, d.headerFields, DefaultEndpointLimits().Headers); err != context.Canceled {
		t.Fatal("cancellation lost", err)
	}
}

func TestRequestHeaderVaryThroughHeadAndConditionalResponses(t *testing.T) {
	e := DefineEndpoint(runtimeHeaderEndpoint().route, runtimeHeaders(), EmptyBody(), JSONResponse(200, endpointReplyJSON()))
	e = EndpointHeaders(e, func(context.Context, EndpointReply) ([]ResponseHeader, error) {
		return []ResponseHeader{{Name: "Vary", Value: "x-note, Accept-Encoding"}}, nil
	})
	router, err := NewRouter(e.Handle(func(context.Context, runtimeHeaderInput) (EndpointReply, error) {
		return EndpointReply{Name: "same"}, nil
	}))
	if err != nil {
		t.Fatal(err)
	}
	handler, err := ApplyMiddleware(router, CORS(CORSConfig{Origins: []Origin{"https://client.test"}, Methods: []Method{GET}}), ETags(DefaultETagConfig()))
	if err != nil {
		t.Fatal(err)
	}
	call := func(method, tag string) *httptest.ResponseRecorder {
		r := httptest.NewRequest(method, "/headers?X-Revision=query", nil)
		r.Header.Set("X-Revision", "1")
		if tag != "" {
			r.Header.Set("If-None-Match", tag)
		}
		w := httptest.NewRecorder()
		handler.ServeHTTP(w, r)
		return w
	}
	first := call("GET", "")
	if first.Code != 200 || first.Header().Get("ETag") == "" {
		t.Fatalf("missing ETag: %d", first.Code)
	}
	for _, w := range []*httptest.ResponseRecorder{first, call("HEAD", ""), call("GET", first.Header().Get("ETag"))} {
		if w.Code != 200 && w.Code != 304 {
			t.Fatalf("unexpected status %d", w.Code)
		}
		assertHeaderVary(t, w.Header(), "X-Note", "X-Revision", "X-Enabled", "Origin", "Accept-Encoding")
	}
	if w := call("GET", first.Header().Get("ETag")); w.Code != 304 || w.Body.Len() != 0 {
		t.Fatal("conditional response failed")
	}
}

func TestRequestHeaderIssueBoundsAndPointerEscaping(t *testing.T) {
	d := DefineQuery(
		HeaderParam("X~Name", IntegerQuery[int](), func(q *runtimeHeaderParams) *int { return &q.Revision }),
		HeaderParam("A", StringQuery[string](), func(q *runtimeHeaderParams) *string { return &q.Query }),
		OptionalHeaderParam("Z", StringQuery[string](), func(q *runtimeHeaderParams) *value.Optional[string] { return &q.Note }),
	)
	limits := DefaultEndpointLimits().Headers
	limits.Issues = 2
	_, err := readRequestHeaders(t.Context(), stdhttp.Header{"X~Name": {"1", "2"}, "Z": {"\x7f"}}, d.headerIndexes, d.headerFields, limits)
	var failure *RequestHeaderError
	if !errors.As(err, &failure) {
		t.Fatal("missing header error", err)
	}
	issues := failure.Issues()
	if len(issues) != 2 || issues[0].Path != "/A" || issues[1].Path != "/X~0name" {
		t.Fatalf("unordered, unbounded or unescaped issues: %+v", issues)
	}
}

func TestRequestHeaderSignedAndConsumedQueriesStaySeparate(t *testing.T) {
	now := testkit.NewClock(urlTestTime)
	d := DefineQuery(HeaderParam("X-Note", StringQuery[string](), func(q *runtimeHeaderParams) *string { return &q.Query }))
	e := DefineEndpoint(runtimeHeaderEndpoint().route, d, EmptyBody(), EmptyResponse(204))
	signed := ConfigureSigned(Signed(e, urlTestSigner(t, now)), IgnoredParameters("X-Note"))
	location, err := signed.URL(t.Context(), urlTestOrigin, NoPath{}, runtimeHeaderParams{}, now.Now().Add(time.Minute))
	if err != nil {
		t.Fatal(err)
	}
	var got string
	router, err := NewRouter(signed.Handle(func(_ context.Context, in runtimeHeaderInput) (NoContent, error) {
		got = in.Query.Query
		return NoContent{}, nil
	}))
	if err != nil {
		t.Fatal(err)
	}
	handler, err := ApplyMiddleware(router, PublicURLs(PublicURLConfig{AllowedOrigins: []Origin{urlTestOrigin}}))
	if err != nil {
		t.Fatal(err)
	}
	for _, text := range []string{"one", "two"} {
		r := httptest.NewRequest("GET", location+"&X-Note=unsigned-query", nil)
		r.Header.Set("X-Note", text)
		w := httptest.NewRecorder()
		handler.ServeHTTP(w, r)
		if w.Code != 204 || got != text {
			t.Fatalf("header affected URL signature: %d %s", w.Code, w.Body.String())
		}
	}
	ctx := withConsumedQueryParameter(t.Context(), "X-Note")
	if raw := queryWithoutConsumed(d, ctx, "X-Note=locale"); raw != "" {
		t.Fatal("header declaration blocked consumed query", raw)
	}
}
