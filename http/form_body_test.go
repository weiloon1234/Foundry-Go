package http

import (
	"context"
	"github.com/weiloon1234/Foundry-Go/value"
	"net/http/httptest"
	"reflect"
	"strings"
	"testing"
)

type formInput struct {
	Name  string
	Alias value.Optional[string]
	Tags  []string
	Count value.Optional[int]
}
type formRequest = Input[NoPath, endpointParameters, formInput]

func formDescriptor() Query[formInput] {
	return DefineQuery(
		QueryParam("name", StringQuery[string](), func(b *formInput) *string { return &b.Name }),
		OptionalQueryParam("alias", StringQuery[string](), func(b *formInput) *value.Optional[string] { return &b.Alias }),
		RepeatedQueryParam("tags[]", StringQuery[string](), func(b *formInput) *[]string { return &b.Tags }),
		OptionalQueryParam("count", IntegerQuery[int](), func(b *formInput) *value.Optional[int] { return &b.Count }),
	)
}
func formEndpoint() Endpoint[NoPath, endpointParameters, formInput, NoContent] {
	route := DefineRoute(RouteSpec{ID: "form.submit", Method: POST, Access: Public}, StaticPath("/form"))
	query := DefineQuery(OptionalQueryParam("name", StringQuery[string](), func(q *endpointParameters) *value.Optional[string] { return &q.Term }))
	return DefineEndpoint(route, query, FormBody(formDescriptor()), EmptyResponse(204))
}
func TestFormBodyWireAndIndependentSources(t *testing.T) {
	for _, tc := range []struct {
		wire, name string
		alias      bool
		tags       []string
	}{
		{"name=Jane+Doe%2B", "Jane Doe+", false, nil},
		{"name&alias=", "", true, nil},
		{"name=null", "null", false, nil},
		{"name=%E2%9C%93&tags%5B%5D=a&tags%5B%5D=b", "✓", false, []string{"a", "b"}},
	} {
		t.Run(tc.wire, func(t *testing.T) {
			called := false
			router, err := NewRouter(formEndpoint().Handle(func(_ context.Context, in formRequest) (NoContent, error) {
				called = true
				query, _ := in.Query.Term.Get()
				if in.Body.Name != tc.name || in.Body.Alias.IsSet() != tc.alias || !reflect.DeepEqual(in.Body.Tags, tc.tags) || query != "query value" {
					t.Errorf("decoded input: %+v", in)
				}
				return NoContent{}, nil
			}))
			if err != nil {
				t.Fatal(err)
			}
			request := httptest.NewRequest("POST", "/form?name=query+value", strings.NewReader(tc.wire))
			request.Header.Set("Content-Type", "application/x-www-form-urlencoded; charset=UTF-8")
			response := httptest.NewRecorder()
			router.ServeHTTP(response, request)
			if response.Code != 204 || !called {
				t.Fatalf("%d %s", response.Code, response.Body.String())
			}
			first := router.Endpoints()
			if first[0].Body.MediaType != "application/x-www-form-urlencoded" || len(first[0].Body.Form) != 4 {
				t.Fatal("missing form metadata")
			}
			first[0].Body.Form[0].Name = "mutated"
			if router.Endpoints()[0].Body.Form[0].Name == "mutated" {
				t.Fatal("metadata aliases descriptor")
			}
		})
	}
}
func TestFormBodyFailures(t *testing.T) {
	for _, tc := range []struct {
		name, wire, media, encoding, path string
		status                            int
	}{
		{"missing", "alias=ok", "application/x-www-form-urlencoded", "", "/body/name", 400},
		{"duplicate", "name=a&%6eame=b", "application/x-www-form-urlencoded", "", "/body/name", 400},
		{"unknown", "name=a&private-key=private-value", "application/x-www-form-urlencoded", "", "/body", 400},
		{"implicit-nesting", "name[child]=private-value", "application/x-www-form-urlencoded", "", "/body", 400},
		{"bad-percent", "name=%xx", "application/x-www-form-urlencoded", "", "/body", 400},
		{"bad-utf8", "name=%ff", "application/x-www-form-urlencoded", "", "/body", 400},
		{"wrong-type", "name=ok&count=hello", "application/x-www-form-urlencoded", "", "/body/count", 400},
		{"charset", "name=ok", "application/x-www-form-urlencoded;charset=latin1", "", "", 415},
		{"wrong-media", "name=ok", "application/json", "", "", 415},
		{"compression", "name=ok", "application/x-www-form-urlencoded", "gzip", "", 415},
	} {
		t.Run(tc.name, func(t *testing.T) {
			router, err := NewRouter(formEndpoint().Handle(func(context.Context, formRequest) (NoContent, error) {
				t.Error("invalid form reached handler")
				return NoContent{}, nil
			}))
			if err != nil {
				t.Fatal(err)
			}
			req := httptest.NewRequest("POST", "/form?name=cannot+satisfy+body", strings.NewReader(tc.wire))
			req.Header.Set("Content-Type", tc.media)
			if tc.encoding != "" {
				req.Header.Set("Content-Encoding", tc.encoding)
			}
			res := httptest.NewRecorder()
			router.ServeHTTP(res, req)
			if res.Code != tc.status {
				t.Fatalf("status %d: %s", res.Code, res.Body.String())
			}
			if strings.Contains(res.Body.String(), "private-") {
				t.Fatal("reflected unknown field")
			}
			if tc.path != "" {
				failure := decodeFailure(t, res)
				if len(failure.Issues) == 0 || failure.Issues[0].Path != tc.path {
					t.Fatalf("issues: %+v", failure.Issues)
				}
			}
		})
	}
}
func TestFormBodyBoundsAndCompatibility(t *testing.T) {
	for _, tc := range []struct {
		name, wire   string
		bytes, pairs int
		stream       bool
		status       int
	}{
		{"known-bytes", "name=123456789", 8, 4, false, 413},
		{"streamed-bytes", "name=123456789", 8, 4, true, 413},
		{"pairs", "name=x&alias=y", 64, 1, false, 400},
		{"independent-json", "name=abcdef", 64, 4, false, 204},
	} {
		t.Run(tc.name, func(t *testing.T) {
			limits := DefaultEndpointLimits()
			limits.Body.Bytes = 1
			limits.Form = QueryLimits{Bytes: tc.bytes, Pairs: tc.pairs, Issues: 1}
			calls := 0
			router, err := NewRouter(formEndpoint().WithLimits(limits).Handle(func(context.Context, formRequest) (NoContent, error) { calls++; return NoContent{}, nil }))
			if err != nil {
				t.Fatal(err)
			}
			req := httptest.NewRequest("POST", "/form", strings.NewReader(tc.wire))
			if tc.stream {
				req.ContentLength = -1
			}
			req.Header.Set("Content-Type", "application/x-www-form-urlencoded")
			res := httptest.NewRecorder()
			router.ServeHTTP(res, req)
			if res.Code != tc.status || (calls == 1) != (tc.status == 204) {
				t.Fatalf("%d calls=%d: %s", res.Code, calls, res.Body.String())
			}
		})
	}
	limits := DefaultEndpointLimits()
	limits.Form = QueryLimits{}
	if err := patchEndpoint().WithLimits(limits).Validate(); err != nil {
		t.Fatal("old non-form limits invalid", err)
	}
	if err := formEndpoint().WithLimits(limits).Validate(); err == nil {
		t.Fatal("form with no budget accepted")
	}
}
func FuzzFormEndpointBounds(f *testing.F) {
	for _, seed := range []string{"name=ok", "name=%ff", "name=a&name=b", "name=x&tags[]=a&tags[]=b", "name[child]=x", "name=%"} {
		f.Add(seed)
	}
	limits := DefaultEndpointLimits()
	limits.Form = QueryLimits{Bytes: 1024, Pairs: 16, Issues: 3}
	router, err := NewRouter(formEndpoint().WithLimits(limits).Handle(func(context.Context, formRequest) (NoContent, error) { return NoContent{}, nil }))
	if err != nil {
		f.Fatal(err)
	}
	f.Fuzz(func(t *testing.T, wire string) {
		if len(wire) > 2048 {
			return
		}
		req := httptest.NewRequest("POST", "/form", strings.NewReader(wire))
		req.Header.Set("Content-Type", "application/x-www-form-urlencoded")
		res := httptest.NewRecorder()
		router.ServeHTTP(res, req)
		if res.Code != 204 && res.Code != 400 && res.Code != 413 {
			t.Fatalf("unexpected status %d", res.Code)
		}
		if res.Code != 204 && len(decodeFailure(t, res).Issues) > limits.Form.Issues {
			t.Fatal("issue limit exceeded")
		}
	})
}

func TestFormExplicitCompositionAndBoundedDiagnostics(t *testing.T) {
	type inner struct{ Name string }
	type outer struct{ Contact inner }
	fields := DefineQuery(QueryParam("contact.name", StringQuery[string](), func(v *inner) *string { return &v.Name }))
	descriptor := MergeQueries(EmbedQuery(fields, func(v *outer) *inner { return &v.Contact }))
	endpoint := DefineEndpoint(DefineRoute(RouteSpec{ID: "form.nested", Method: POST, Access: Public}, StaticPath("/nested")), EmptyQuery(), FormBody(descriptor), EmptyResponse(204))
	limits := DefaultEndpointLimits()
	limits.Form.Issues = 1
	called := false
	router, err := NewRouter(endpoint.WithLimits(limits).Handle(func(_ context.Context, in Input[NoPath, NoQuery, outer]) (NoContent, error) {
		called = true
		if in.Body.Contact.Name != "Jane" {
			t.Error("lost concrete nested value")
		}
		return NoContent{}, nil
	}))
	if err != nil {
		t.Fatal(err)
	}
	for _, wire := range []string{"contact.name=Jane", "contact[deep][key]=secret&private=secret&another=secret"} {
		called = false
		req := httptest.NewRequest("POST", "/nested", strings.NewReader(wire))
		req.Header.Set("Content-Type", "application/x-www-form-urlencoded")
		res := httptest.NewRecorder()
		router.ServeHTTP(res, req)
		if strings.HasPrefix(wire, "contact.name=") {
			if res.Code != 204 || !called {
				t.Fatal(res.Code, res.Body.String())
			}
		} else {
			failure := decodeFailure(t, res)
			if res.Code != 400 || called || len(failure.Issues) != 1 || failure.Issues[0].Path != "/body" {
				t.Fatal(res.Code, failure)
			}
		}
	}
}
