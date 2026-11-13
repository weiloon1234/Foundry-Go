package http_test

import (
	"context"
	"encoding/json"
	"errors"
	"reflect"
	"strings"
	"testing"

	"github.com/weiloon1234/Foundry-Go/contract"
	"github.com/weiloon1234/Foundry-Go/fault"
	foundryhttp "github.com/weiloon1234/Foundry-Go/http"
	"github.com/weiloon1234/Foundry-Go/value"
)

type headerParameters struct {
	Query   string
	Version string
	Device  value.Optional[string]
}

func headerVersion(name foundryhttp.HeaderName) foundryhttp.QueryParameter[headerParameters] {
	return foundryhttp.HeaderParam(name, foundryhttp.StringQuery[string](), func(q *headerParameters) *string { return &q.Version })
}

func mixedHeaderParameters() foundryhttp.Query[headerParameters] {
	return foundryhttp.DefineQuery(
		foundryhttp.OptionalHeaderParam("x-device-ID", foundryhttp.StringQuery[string](), func(q *headerParameters) *value.Optional[string] { return &q.Device }),
		headerVersion("x-app-version"),
		foundryhttp.QueryParam("X-App-Version", foundryhttp.StringQuery[string](), func(q *headerParameters) *string { return &q.Query }),
	)
}

func headerEndpoint(query foundryhttp.Query[headerParameters], options ...foundryhttp.EndpointOption) foundryhttp.Endpoint[foundryhttp.NoPath, headerParameters, foundryhttp.NoBody, foundryhttp.NoContent] {
	route := foundryhttp.DefineRoute(foundryhttp.RouteSpec{ID: "headers.inspect", Method: foundryhttp.GET, Access: foundryhttp.Public}, foundryhttp.StaticPath("/headers"))
	return foundryhttp.DefineEndpoint(route, query, foundryhttp.EmptyBody(), foundryhttp.EmptyResponse(204), options...)
}

func TestRequestHeaderDeclarationsRejectReservedNames(t *testing.T) {
	for _, name := range []string{
		"Authorization", "Cookie", "Content-Type", "Content-Custom", "Host", "Connection",
		"Transfer-Encoding", "Origin", "Sec-Fetch-Site", "Sec-Custom", "Idempotency-Key",
		"X-CSRF-Token", "X-XSRF-Token", "Accept-Encoding", "Access-Control-Custom",
		"If-Match", "Forwarded", "X-Forwarded-For", "X-Request-ID", "Traceparent",
	} {
		for _, spelling := range []string{name, strings.ToUpper(name), strings.ToLower(name)} {
			descriptor := foundryhttp.DefineQuery(headerVersion(foundryhttp.HeaderName(spelling)))
			if err := foundryhttp.ValidateQueryDescriptor(descriptor); !errors.Is(err, fault.Invalid) {
				t.Errorf("accepted reserved header %q: %v", spelling, err)
			}
			if err := foundryhttp.ValidateEndpoint(headerEndpoint(descriptor)); err == nil {
				t.Errorf("registered reserved header %q", spelling)
			}
		}
	}
}

func TestRequestHeaderDeclarationsRejectInvalidAndDuplicateBindings(t *testing.T) {
	for _, name := range []foundryhttp.HeaderName{"", "bad name", "x:y", "é", foundryhttp.HeaderName(strings.Repeat("x", 257))} {
		if err := foundryhttp.ValidateQueryDescriptor(foundryhttp.DefineQuery(headerVersion(name))); err == nil {
			t.Errorf("accepted invalid name %q", name)
		}
	}
	for _, second := range []foundryhttp.HeaderName{"X-App-Version", "x-app-version", "X-APP-VERSION"} {
		if err := foundryhttp.ValidateQueryDescriptor(foundryhttp.DefineQuery(headerVersion("X-App-Version"), headerVersion(second))); err == nil {
			t.Errorf("accepted duplicate name %q", second)
		}
	}
	invalid := []foundryhttp.Query[headerParameters]{
		{},
		foundryhttp.DefineQuery(foundryhttp.HeaderParam[headerParameters, string]("X-App-Version", foundryhttp.StringQuery[string](), nil)),
		foundryhttp.DefineQuery(foundryhttp.OptionalHeaderParam[headerParameters, string]("X-Device-ID", foundryhttp.StringQuery[string](), nil)),
		foundryhttp.DefineQuery(foundryhttp.HeaderParam("X-App-Version", nil, func(q *headerParameters) *string { return &q.Version })),
	}
	for _, descriptor := range invalid {
		if _, err := foundryhttp.RequestHeaderParameters(descriptor); err == nil {
			t.Error("inspected an invalid header descriptor")
		}
	}
}

func TestRequestHeaderInspectionAndQueryProjection(t *testing.T) {
	descriptor := mixedHeaderParameters()
	if err := foundryhttp.ValidateQueryDescriptor(descriptor); err != nil {
		t.Fatal("query and header with the same spelling must coexist", err)
	}
	query, err := foundryhttp.QueryParameters(descriptor)
	if err != nil || len(query) != 1 || query[0].Name != "X-App-Version" {
		t.Fatalf("query projection: %+v, %v", query, err)
	}
	headers, err := foundryhttp.RequestHeaderParameters(descriptor)
	if err != nil || len(headers) != 2 {
		t.Fatalf("header projection: %+v, %v", headers, err)
	}
	if headers[0].Name != "X-App-Version" || !headers[0].Required || headers[1].Name != "X-Device-Id" || headers[1].Required {
		t.Fatalf("canonical order/cardinality: %+v", headers)
	}
	for _, header := range headers {
		if header.Repeated || header.DefaultURL.IsSet() || header.Scalar == nil || header.Scalar.Syntax != foundryhttp.TextURLSyntax {
			t.Fatalf("header scalar metadata: %+v", header)
		}
	}
	got, err := foundryhttp.DecodeQuery(descriptor, t.Context(), "X-App-Version=query", foundryhttp.DefaultEndpointLimits().Query)
	if err != nil || got.Query != "query" || got.Version != "" || got.Device.IsSet() {
		t.Fatalf("query decoding crossed sources: %+v, %v", got, err)
	}
	wire, err := foundryhttp.EncodeQuery(descriptor, t.Context(), headerParameters{Query: "query", Version: "private", Device: value.Set("device")}, foundryhttp.DefaultEndpointLimits().Query)
	if err != nil || wire != "X-App-Version=query" {
		t.Fatalf("query encoding crossed sources: %q, %v", wire, err)
	}
	if _, err := foundryhttp.DecodeQuery(descriptor, t.Context(), "X-App-Version=query&X-Device-Id=device", foundryhttp.DefaultEndpointLimits().Query); err == nil {
		t.Fatal("header name admitted an undeclared query key")
	}
	empty, err := foundryhttp.RequestHeaderParameters(foundryhttp.EmptyQuery())
	if err != nil || empty == nil || len(empty) != 0 {
		t.Fatalf("empty header metadata: %#v, %v", empty, err)
	}
}

func TestRequestHeaderCompositionPreservesBothSources(t *testing.T) {
	type outer struct{ Inner headerParameters }
	inner := mixedHeaderParameters()
	embedded := foundryhttp.EmbedQuery(inner, func(q *outer) *headerParameters { return &q.Inner })
	merged := foundryhttp.MergeQueries(foundryhttp.DefineQuery[outer](), embedded)
	wantHeaders, _ := foundryhttp.RequestHeaderParameters(inner)
	gotHeaders, err := foundryhttp.RequestHeaderParameters(merged)
	if err != nil || !reflect.DeepEqual(gotHeaders, wantHeaders) {
		t.Fatalf("embedded headers: %+v, %v", gotHeaders, err)
	}
	wantQuery, _ := foundryhttp.QueryParameters(inner)
	gotQuery, err := foundryhttp.QueryParameters(merged)
	if err != nil || !reflect.DeepEqual(gotQuery, wantQuery) {
		t.Fatalf("embedded query: %+v, %v", gotQuery, err)
	}
	if err := foundryhttp.ValidateQueryDescriptor(foundryhttp.MergeQueries(inner, foundryhttp.DefineQuery(headerVersion("X-APP-VERSION")))); err == nil {
		t.Fatal("merge accepted duplicate header")
	}
}

func TestRequestHeaderBindingsCannotBecomeBodyFields(t *testing.T) {
	for _, descriptor := range []foundryhttp.Query[headerParameters]{mixedHeaderParameters(), foundryhttp.DefineQuery(headerVersion("X-App-Version"))} {
		if err := foundryhttp.ValidateBodyDescriptor(foundryhttp.FormBody(descriptor)); err == nil {
			t.Fatal("form accepted a header binding")
		}
	}
	part := foundryhttp.TextPart(headerVersion("X-App-Version"))
	multipart := foundryhttp.DefineMultipart(part)
	if err := foundryhttp.ValidateMultipart(multipart); err == nil {
		t.Fatal("multipart accepted a header binding")
	}
	if _, err := foundryhttp.InspectMultipart(multipart); err == nil {
		t.Fatal("multipart exported an invalid header binding")
	}
	defaults := foundryhttp.DefineQuery(foundryhttp.DefaultQueryParam("version", foundryhttp.StringQuery[string](), "1", func(q *headerParameters) *string { return &q.Version }))
	if err := foundryhttp.ValidateBodyDescriptor(foundryhttp.FormBody(defaults)); err != nil {
		t.Fatal("existing form defaults changed", err)
	}
}

func TestRequestHeaderEndpointMetadataOwnsSnapshots(t *testing.T) {
	cases := []json.RawMessage{json.RawMessage(`"ready"`)}
	codec := foundryhttp.DescribeURL(foundryhttp.StringQuery[string](), contract.DefineScalar[string](contract.Type{ID: "HeaderState", Kind: contract.StringKind, Cases: cases}))
	parameter := foundryhttp.HeaderParam("x-state", codec, func(q *headerParameters) *string { return &q.Version })
	declarations := []foundryhttp.QueryParameter[headerParameters]{parameter}
	descriptor := foundryhttp.DefineQuery(declarations...)
	declarations[0] = headerVersion("X-Replaced")
	cases[0][1] = 'x'
	endpoint := headerEndpoint(descriptor)
	info, err := foundryhttp.InspectEndpoint(endpoint)
	if err != nil || len(info.Headers) != 1 || len(info.Query) != 0 {
		t.Fatalf("endpoint metadata: %+v, %v", info, err)
	}
	if info.Headers[0].Name != "X-State" || string(info.Headers[0].Scalar.Value.Cases[0]) != `"ready"` {
		t.Fatal("declaration was not snapshotted")
	}
	info.Headers[0].Name = "X-Mutated"
	info.Headers[0].Scalar.Value.Cases[0][1] = 'y'
	again, err := foundryhttp.InspectEndpoint(endpoint)
	if err != nil || again.Headers[0].Name != "X-State" || string(again.Headers[0].Scalar.Value.Cases[0]) != `"ready"` {
		t.Fatal("endpoint inspection aliased retained metadata", err)
	}
	router, err := foundryhttp.NewRouter(endpoint.Handle(func(context.Context, foundryhttp.Input[foundryhttp.NoPath, headerParameters, foundryhttp.NoBody]) (foundryhttp.NoContent, error) {
		return foundryhttp.NoContent{}, nil
	}))
	if err != nil {
		t.Fatal(err)
	}
	registered := router.Endpoints()
	if len(registered) != 1 || !reflect.DeepEqual(registered[0].Headers, again.Headers) {
		t.Fatal("registered endpoint omitted header metadata")
	}
	registered[0].Headers[0].Scalar.Value.Cases[0][1] = 'z'
	if string(router.Endpoints()[0].Headers[0].Scalar.Value.Cases[0]) != `"ready"` {
		t.Fatal("router inspection aliased header scalar cases")
	}
}

func TestRequestHeaderLimitsAndRegistration(t *testing.T) {
	defaults := foundryhttp.DefaultEndpointLimits()
	want := foundryhttp.RequestHeaderLimits{FieldBytes: foundryhttp.MaxHeaderValueBytes, Bytes: 16 << 10, Fields: 32, Values: 64, Issues: 16}
	if defaults.Headers != want || defaults.Headers.Validate() != nil {
		t.Fatalf("header defaults: %+v", defaults.Headers)
	}
	for _, change := range []func(*foundryhttp.RequestHeaderLimits){
		func(l *foundryhttp.RequestHeaderLimits) { l.FieldBytes = 0 },
		func(l *foundryhttp.RequestHeaderLimits) { l.Bytes = 0 },
		func(l *foundryhttp.RequestHeaderLimits) { l.Fields = 0 },
		func(l *foundryhttp.RequestHeaderLimits) { l.Values = 0 },
		func(l *foundryhttp.RequestHeaderLimits) { l.Issues = 0 },
		func(l *foundryhttp.RequestHeaderLimits) { l.FieldBytes = foundryhttp.MaxHeaderValueBytes + 1 },
		func(l *foundryhttp.RequestHeaderLimits) { l.Bytes = l.FieldBytes - 1 },
		func(l *foundryhttp.RequestHeaderLimits) { l.Values = l.Fields - 1 },
	} {
		limits := defaults
		change(&limits.Headers)
		if limits.Headers.Validate() == nil || limits.Validate() == nil {
			t.Errorf("accepted invalid header budget: %+v", limits.Headers)
		}
	}
	zero := defaults
	zero.Headers = foundryhttp.RequestHeaderLimits{}
	if zero.Validate() != nil || foundryhttp.ValidateEndpoint(headerEndpoint(foundryhttp.DefineQuery[headerParameters](), foundryhttp.Limits(zero))) != nil {
		t.Fatal("inactive header budget broke header-free endpoints")
	}
	if err := foundryhttp.ValidateEndpoint(headerEndpoint(mixedHeaderParameters(), foundryhttp.Limits(zero))); err == nil {
		t.Fatal("header declaration accepted an inactive budget")
	}
	for _, limits := range []foundryhttp.RequestHeaderLimits{
		{FieldBytes: 100, Bytes: 100, Fields: 1, Values: 1, Issues: 1},
		{FieldBytes: 1, Bytes: 2, Fields: 2, Values: 2, Issues: 1},
	} {
		settings := defaults
		settings.Headers = limits
		if err := foundryhttp.ValidateEndpoint(headerEndpoint(mixedHeaderParameters(), foundryhttp.Limits(settings))); err == nil {
			t.Errorf("accepted declarations beyond budget: %+v", limits)
		}
	}
}
