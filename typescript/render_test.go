package typescript_test

import (
	"bytes"
	"context"
	"strings"
	"testing"

	"github.com/weiloon1234/Foundry-Go/contract"
	"github.com/weiloon1234/Foundry-Go/contract/manifest"
	foundryhttp "github.com/weiloon1234/Foundry-Go/http"
	"github.com/weiloon1234/Foundry-Go/openapi"
	"github.com/weiloon1234/Foundry-Go/typescript"
)

func clientManifest(t *testing.T) *manifest.Manifest {
	t.Helper()
	extra := contract.Schema{Root: "consumer.Payload", Types: []contract.Type{
		{ID: "consumer.Payload", Kind: contract.ObjectKind, Properties: []contract.Property{{Name: "required_nullable", Type: "consumer.Nullable", Required: true}, {Name: "optional", Type: "string"}, {Name: "wide", Type: "consumer.Integer", Required: true}, {Name: "quoted", Type: "consumer.Quoted", Required: true}}},
		{ID: "string", Kind: contract.StringKind}, {ID: "consumer.Nullable", Kind: contract.AliasKind, Element: "string", Nullable: true}, {ID: "consumer.Integer", Kind: contract.IntegerKind, Bits: 64, Signed: true}, {ID: "consumer.Quoted", Kind: contract.QuotedKind, Element: "consumer.Integer"},
	}}
	endpoint := foundryhttp.DefineEndpoint(foundryhttp.DefineRoute(foundryhttp.RouteSpec{ID: "health.read", Method: foundryhttp.GET, Access: foundryhttp.Public}, foundryhttp.StaticPath("/health")), foundryhttp.EmptyQuery(), foundryhttp.EmptyBody(), foundryhttp.JSONResponse(201, contract.StringJSON[string]()))
	router, err := foundryhttp.NewRouter(endpoint.Handle(func(context.Context, foundryhttp.Input[foundryhttp.NoPath, foundryhttp.NoQuery, foundryhttp.NoBody]) (string, error) {
		t.Fatal("export ran a handler")
		return "", nil
	}))
	if err != nil {
		t.Fatal(err)
	}
	source, err := manifest.Build(t.Context(), manifest.Sources{HTTP: router, Schemas: []contract.Schema{extra}})
	if err != nil {
		t.Fatal(err)
	}
	return source
}

func TestRenderOwnsStableTypedClientAndExactManifest(t *testing.T) {
	source := clientManifest(t)
	first, err := typescript.Render(source)
	if err != nil {
		t.Fatal(err)
	}
	second, err := typescript.Render(source)
	if err != nil || !bytes.Equal(first, second) {
		t.Fatal("unstable SDK", err)
	}
	for _, expected := range []string{`readonly "required_nullable":`, `readonly "optional"?:`, `"healthRead"`, `new JSONNumber`, `Promise<Operations["healthRead"]["response"]>`, `"consumer.Payload"`, `export function contractMetadata`, `export class ContractError`} {
		if !strings.Contains(string(first), expected) {
			t.Fatal("missing generated contract", expected)
		}
	}
	first[0] = '!'
	third, err := typescript.Render(source)
	if err != nil || !bytes.Equal(second, third) {
		t.Fatal("caller mutation changed renderer", err)
	}
	if _, err := typescript.Render(nil); err == nil {
		t.Fatal("nil manifest accepted")
	}
}

func TestClientGenerationPublishesAllAdaptersTogether(t *testing.T) {
	source := clientManifest(t)
	options := typescript.Options{Dir: t.TempDir(), Prefix: "api", OpenAPI: openapi.Options{Title: "Consumer", APIVersion: "1"}}
	report, err := typescript.Generate(t.Context(), source, options)
	if err != nil || len(report.Written) != 3 {
		t.Fatal(report, err)
	}
	options.Check = true
	report, err = typescript.Generate(t.Context(), source, options)
	if err != nil || len(report.Written) != 0 {
		t.Fatal(report, err)
	}
	options.Prefix = "next"
	if _, err := typescript.Generate(t.Context(), source, options); err == nil {
		t.Fatal("rename accepted by check")
	}
	options.Check = false
	report, err = typescript.Generate(t.Context(), source, options)
	if err != nil || len(report.Written) != 3 || len(report.Removed) != 3 {
		t.Fatal(report, err)
	}
	options.Prefix = "../bad"
	if _, err := typescript.Generate(t.Context(), source, options); err == nil {
		t.Fatal("unsafe prefix accepted")
	}
}

func TestRenderMapWithoutKeyConstraint(t *testing.T) {
	schema := contract.Schema{Root: "example.Labels", Types: []contract.Type{
		{ID: "example.Labels", Kind: contract.MapKind, Element: "string", Nullable: true},
		{ID: "string", Kind: contract.StringKind},
	}}
	source, err := manifest.Build(t.Context(), manifest.Sources{Schemas: []contract.Schema{schema}})
	if err != nil {
		t.Fatal(err)
	}
	data, err := source.JSON()
	if err != nil {
		t.Fatal(err)
	}
	restored, err := manifest.Decode(data)
	if err != nil {
		t.Fatal(err)
	}
	for _, current := range []*manifest.Manifest{source, restored} {
		if _, err := typescript.Render(current); err != nil {
			t.Fatal(err)
		}
		if _, err := openapi.Render(current, openapi.Options{Title: "Maps", APIVersion: "1"}); err != nil {
			t.Fatal(err)
		}
	}
}

func TestSchemaNamesAvoidRuntimeDeclarations(t *testing.T) {
	colliding := contract.Schema{Root: "consumer/api.Payload", Types: []contract.Type{
		{ID: "consumer/api.Payload", Kind: contract.ObjectKind, Properties: []contract.Property{{Name: "map", Type: "consumer/api.Map", Required: true}}},
		{ID: "consumer/api.Map", Kind: contract.StringKind},
	}}
	source, err := manifest.Build(t.Context(), manifest.Sources{Schemas: []contract.Schema{colliding}})
	if err != nil {
		t.Fatal(err)
	}
	sdk, err := typescript.Render(source)
	if err != nil {
		t.Fatal(err)
	}
	for _, want := range []string{"export type Api_Payload = ", "export type Api_Map = string;", `readonly "consumer/api.Payload": Api_Payload;`} {
		if !strings.Contains(string(sdk), want) {
			t.Fatal("reserved runtime name was not qualified", want)
		}
	}
}

func TestRenderTypesStatusesRedirectsAndRawBodies(t *testing.T) {
	route := func(id foundryhttp.RouteID, method foundryhttp.Method, path string) foundryhttp.Route[foundryhttp.NoPath] {
		return foundryhttp.DefineRoute(foundryhttp.RouteSpec{ID: id, Method: method, Access: foundryhttp.Public}, foundryhttp.StaticPath(path))
	}
	type none = foundryhttp.Input[foundryhttp.NoPath, foundryhttp.NoQuery, foundryhttp.NoBody]
	upsert := foundryhttp.DefineEndpoint(route("items.upsert", foundryhttp.PUT, "/upsert"), foundryhttp.EmptyQuery(), foundryhttp.EmptyBody(), foundryhttp.JSONResponses(contract.StringJSON[string](), 200, 201))
	moved := foundryhttp.DefineEndpoint(route("session.continue", foundryhttp.POST, "/continue"), foundryhttp.EmptyQuery(), foundryhttp.EmptyBody(), foundryhttp.RedirectResponse(303))
	events := foundryhttp.DefineEndpoint(route("items.events", foundryhttp.GET, "/events"), foundryhttp.EmptyQuery(), foundryhttp.EmptyBody(), foundryhttp.EventStreamResponse(contract.StringJSON[string]()))
	raw := foundryhttp.DefineEndpoint(route("files.raw", foundryhttp.POST, "/raw"), foundryhttp.EmptyQuery(), foundryhttp.RawRequestBody("application/octet-stream"), foundryhttp.JSONResponse(201, contract.StringJSON[string]()))
	router, err := foundryhttp.NewRouter(
		upsert.Handle(func(context.Context, none) (foundryhttp.Statused[string], error) {
			return foundryhttp.Statused[string]{}, nil
		}),
		moved.Handle(func(context.Context, none) (foundryhttp.Redirect, error) { return foundryhttp.RedirectTo("/"), nil }),
		events.Handle(func(context.Context, none) (foundryhttp.Events[string], error) {
			return foundryhttp.EventsFrom(func(context.Context, *foundryhttp.EventSink[string]) error { return nil }), nil
		}),
		raw.Handle(func(context.Context, foundryhttp.Input[foundryhttp.NoPath, foundryhttp.NoQuery, foundryhttp.RawBody]) (string, error) {
			return "", nil
		}),
	)
	if err != nil {
		t.Fatal(err)
	}
	source, err := manifest.Build(t.Context(), manifest.Sources{HTTP: router})
	if err != nil {
		t.Fatal(err)
	}
	output, err := typescript.Render(source)
	if err != nil {
		t.Fatal(err)
	}
	for _, expected := range []string{`readonly response: StatusResult<200 | 201, `, `readonly response: RedirectResult<303>`, `readonly response: EventStreamResult<`, `readonly body: { readonly data: RawData; readonly mediaType?: "application/octet-stream" }`} {
		if !strings.Contains(string(output), expected) {
			t.Fatal("missing generated contract", expected)
		}
	}
}
