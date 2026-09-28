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
