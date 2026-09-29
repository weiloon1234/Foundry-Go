package openapi_test

import (
	"bytes"
	"context"
	"encoding/json"
	"strings"
	"testing"

	"github.com/weiloon1234/Foundry-Go/contract"
	"github.com/weiloon1234/Foundry-Go/contract/manifest"
	foundryhttp "github.com/weiloon1234/Foundry-Go/http"
	"github.com/weiloon1234/Foundry-Go/openapi"
)

func TestOpenAPIUsesManifestStatusReferencesAndExactWireShapes(t *testing.T) {
	extra := contract.Schema{Root: "example.Payload", Types: []contract.Type{
		{ID: "example.Payload", Kind: contract.ObjectKind, Properties: []contract.Property{{Name: "required_nullable", Type: "example.Nullable", Required: true}, {Name: "optional_nonnull", Type: "string"}, {Name: "wide", Type: "example.Wide", Required: true}}},
		{ID: "example.Nullable", Kind: contract.AliasKind, Element: "string", Nullable: true},
		{ID: "example.Wide", Kind: contract.QuotedKind, Element: "example.Integer"},
		{ID: "example.Integer", Kind: contract.IntegerKind, Bits: 64, Signed: true},
		{ID: "string", Kind: contract.StringKind},
	}}
	endpoint := foundryhttp.DefineEndpoint(foundryhttp.DefineRoute(foundryhttp.RouteSpec{ID: "health.read", Method: foundryhttp.GET, Access: foundryhttp.Public}, foundryhttp.StaticPath("/health")), foundryhttp.EmptyQuery(), foundryhttp.EmptyBody(), foundryhttp.JSONResponse(201, contract.StringJSON[string]()))
	router, err := foundryhttp.NewRouter(endpoint.Handle(func(context.Context, foundryhttp.Input[foundryhttp.NoPath, foundryhttp.NoQuery, foundryhttp.NoBody]) (string, error) {
		t.Fatal("export invoked handler")
		return "", nil
	}))
	if err != nil {
		t.Fatal(err)
	}
	source, err := manifest.Build(t.Context(), manifest.Sources{HTTP: router, Schemas: []contract.Schema{extra}})
	if err != nil {
		t.Fatal(err)
	}
	options := openapi.Options{Title: "Consumer API", APIVersion: "1"}
	data, err := openapi.Render(source, options)
	if err != nil {
		t.Fatal(err)
	}
	if !bytes.Contains(data, []byte("9223372036854775807")) {
		t.Fatal("integer precision lost")
	}
	again, err := openapi.Render(source, options)
	if err != nil || !bytes.Equal(data, again) {
		t.Fatal("OpenAPI is not deterministic", err)
	}
	var document map[string]any
	decoder := json.NewDecoder(bytes.NewReader(data))
	decoder.UseNumber()
	if err := decoder.Decode(&document); err != nil {
		t.Fatal(err)
	}
	if document["openapi"] != openapi.SpecificationVersion {
		t.Fatal("wrong specification")
	}
	schemas := document["components"].(map[string]any)["schemas"].(map[string]any)
	var payload, nullable, quoted map[string]any
	for _, raw := range schemas {
		schema := raw.(map[string]any)
		switch schema["x-foundry-type-id"] {
		case "example.Payload":
			payload = schema
		case "example.Nullable":
			nullable = schema
		case "example.Wide":
			quoted = schema
		}
	}
	required, _ := payload["required"].([]any)
	if len(required) != 2 || nullable["anyOf"] == nil || quoted["type"] != "string" || quoted["contentSchema"] == nil {
		t.Fatal("optional/null/quoted semantics changed")
	}
	operation := document["paths"].(map[string]any)["/health"].(map[string]any)["get"].(map[string]any)
	responses := operation["responses"].(map[string]any)
	if operation["operationId"] != "health.read" || responses["201"] == nil || responses["200"] != nil || responses["422"] == nil {
		t.Fatal("registered status or errors changed")
	}
	var references func(any)
	references = func(value any) {
		switch value := value.(type) {
		case map[string]any:
			for key, child := range value {
				if key == "$ref" {
					name := strings.TrimPrefix(child.(string), "#/components/schemas/")
					if schemas[name] == nil {
						t.Fatal("dangling OpenAPI reference", name)
					}
				}
				references(child)
			}
		case []any:
			for _, child := range value {
				references(child)
			}
		}
	}
	references(document)
	if _, err := openapi.Render(source, openapi.Options{}); err == nil {
		t.Fatal("missing API identity accepted")
	}
}

func TestHeadOpenAPIHasNoPayloadForSuccessOrFailure(t *testing.T) {
	endpoint := foundryhttp.DefineEndpoint(foundryhttp.DefineRoute(foundryhttp.RouteSpec{ID: "head", Method: foundryhttp.HEAD, Access: foundryhttp.Public}, foundryhttp.StaticPath("/head")), foundryhttp.EmptyQuery(), foundryhttp.EmptyBody(), foundryhttp.JSONResponse(200, contract.StringJSON[string]()))
	router, err := foundryhttp.NewRouter(endpoint.Handle(func(context.Context, foundryhttp.Input[foundryhttp.NoPath, foundryhttp.NoQuery, foundryhttp.NoBody]) (string, error) {
		return "ignored", nil
	}))
	if err != nil {
		t.Fatal(err)
	}
	source, err := manifest.Build(t.Context(), manifest.Sources{HTTP: router})
	if err != nil {
		t.Fatal(err)
	}
	data, err := openapi.Render(source, openapi.Options{Title: "HEAD", APIVersion: "1"})
	if err != nil {
		t.Fatal(err)
	}
	var document map[string]any
	if err := json.Unmarshal(data, &document); err != nil {
		t.Fatal(err)
	}
	responses := document["paths"].(map[string]any)["/head"].(map[string]any)["head"].(map[string]any)["responses"].(map[string]any)
	for _, response := range responses {
		if response.(map[string]any)["content"] != nil {
			t.Fatal("HEAD claims a wire payload")
		}
	}
}

func TestOpenAPIExportsRouteDocumentationExamplesAndServers(t *testing.T) {
	route := foundryhttp.DefineRoute(foundryhttp.RouteSpec{ID: "notes.create", Method: foundryhttp.POST, Access: foundryhttp.Public}, foundryhttp.StaticPath("/notes"))
	endpoint := foundryhttp.DefineEndpoint(route, foundryhttp.EmptyQuery(), foundryhttp.JSONBody(contract.StringJSON[string]()), foundryhttp.JSONResponse(201, contract.StringJSON[string]())).
		WithDocumentation(foundryhttp.RouteDocumentation{Summary: "Create a note", Description: "Stores a note.\nReturns its text.", Tags: []string{"notes", "writes"}, Deprecated: true}).
		WithBodyExample("draft").
		WithResponseExample("stored")
	router, err := foundryhttp.NewRouter(endpoint.Handle(func(context.Context, foundryhttp.Input[foundryhttp.NoPath, foundryhttp.NoQuery, string]) (string, error) {
		t.Fatal("export invoked handler")
		return "", nil
	}))
	if err != nil {
		t.Fatal(err)
	}
	source, err := manifest.Build(t.Context(), manifest.Sources{HTTP: router})
	if err != nil {
		t.Fatal(err)
	}
	if _, err := manifest.Decode(must(source.JSON())); err != nil {
		t.Fatal("documented manifest does not round-trip", err)
	}
	options := openapi.Options{Title: "Notes", APIVersion: "1", Servers: []openapi.Server{{URL: "https://api.example.test/v1", Description: "Production"}, {URL: "/api"}}}
	data, err := openapi.Render(source, options)
	if err != nil {
		t.Fatal(err)
	}
	var document struct {
		Servers []map[string]string `json:"servers"`
		Tags    []map[string]string `json:"tags"`
		Paths   map[string]map[string]struct {
			Summary     string   `json:"summary"`
			Description string   `json:"description"`
			Tags        []string `json:"tags"`
			Deprecated  bool     `json:"deprecated"`
			RequestBody struct {
				Content map[string]struct {
					Example any `json:"example"`
				} `json:"content"`
			} `json:"requestBody"`
			Responses map[string]struct {
				Content map[string]struct {
					Example any `json:"example"`
				} `json:"content"`
			} `json:"responses"`
		} `json:"paths"`
	}
	if err := json.Unmarshal(data, &document); err != nil {
		t.Fatal(err)
	}
	operation := document.Paths["/notes"]["post"]
	if operation.Summary != "Create a note" || operation.Description != "Stores a note.\nReturns its text." || !operation.Deprecated || strings.Join(operation.Tags, ",") != "notes,writes" {
		t.Fatalf("operation documentation = %+v", operation)
	}
	if operation.RequestBody.Content["application/json"].Example != "draft" || operation.Responses["201"].Content["application/json"].Example != "stored" {
		t.Fatal("examples were not exported", operation.RequestBody, operation.Responses["201"])
	}
	if len(document.Servers) != 2 || document.Servers[0]["url"] != "https://api.example.test/v1" || document.Servers[1]["url"] != "/api" || len(document.Tags) != 2 || document.Tags[0]["name"] != "notes" {
		t.Fatalf("servers/tags = %v, %v", document.Servers, document.Tags)
	}
	for _, server := range []openapi.Server{{URL: "ftp://example.test"}, {URL: "https://user:secret@example.test"}, {URL: "https://example.test/?q=1"}, {URL: "api"}, {URL: ""}} {
		if _, err := openapi.Render(source, openapi.Options{Title: "Notes", APIVersion: "1", Servers: []openapi.Server{server}}); err == nil {
			t.Fatalf("invalid server %q accepted", server.URL)
		}
	}
}

func must(data []byte, err error) []byte {
	if err != nil {
		panic(err)
	}
	return data
}
