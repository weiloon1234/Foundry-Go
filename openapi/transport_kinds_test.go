package openapi_test

import (
	"context"
	"encoding/json"
	"strings"
	"testing"

	"github.com/weiloon1234/Foundry-Go/contract"
	"github.com/weiloon1234/Foundry-Go/contract/manifest"
	foundryhttp "github.com/weiloon1234/Foundry-Go/http"
	"github.com/weiloon1234/Foundry-Go/openapi"
)

type noInput = foundryhttp.Input[foundryhttp.NoPath, foundryhttp.NoQuery, foundryhttp.NoBody]

func TestOpenAPIDescribesStatusesRedirectsAndRawBodies(t *testing.T) {
	route := func(id foundryhttp.RouteID, method foundryhttp.Method, path string) foundryhttp.Route[foundryhttp.NoPath] {
		return foundryhttp.DefineRoute(foundryhttp.RouteSpec{ID: id, Method: method, Access: foundryhttp.Public}, foundryhttp.StaticPath(path))
	}
	upsert := foundryhttp.DefineEndpoint(route("items.upsert", foundryhttp.PUT, "/upsert"), foundryhttp.EmptyQuery(), foundryhttp.EmptyBody(), foundryhttp.JSONResponses(contract.StringJSON[string](), 200, 201))
	moved := foundryhttp.DefineEndpoint(route("session.continue", foundryhttp.POST, "/continue"), foundryhttp.EmptyQuery(), foundryhttp.EmptyBody(), foundryhttp.RedirectResponse(303))
	events := foundryhttp.DefineEndpoint(route("items.events", foundryhttp.GET, "/events"), foundryhttp.EmptyQuery(), foundryhttp.EmptyBody(), foundryhttp.EventStreamResponse(contract.StringJSON[string]()))
	raw := foundryhttp.DefineEndpoint(route("files.raw", foundryhttp.POST, "/raw"), foundryhttp.EmptyQuery(), foundryhttp.RawRequestBody("application/octet-stream", "image/png"), foundryhttp.JSONResponse(201, contract.StringJSON[string]()))
	router, err := foundryhttp.NewRouter(
		upsert.Handle(func(context.Context, noInput) (foundryhttp.Statused[string], error) {
			return foundryhttp.Statused[string]{}, nil
		}),
		moved.Handle(func(context.Context, noInput) (foundryhttp.Redirect, error) { return foundryhttp.RedirectTo("/"), nil }),
		events.Handle(func(context.Context, noInput) (foundryhttp.Events[string], error) {
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
	data, err := openapi.Render(source, openapi.Options{Title: "Consumer API", APIVersion: "1"})
	if err != nil {
		t.Fatal(err)
	}
	type media struct {
		Schema map[string]any `json:"schema"`
	}
	type response struct {
		Content map[string]media          `json:"content"`
		Headers map[string]map[string]any `json:"headers"`
	}
	type operation struct {
		RequestBody *struct {
			Content map[string]media `json:"content"`
		} `json:"requestBody"`
		Responses map[string]response `json:"responses"`
	}
	var document struct {
		Paths map[string]map[string]operation `json:"paths"`
	}
	if err := json.Unmarshal(data, &document); err != nil {
		t.Fatal(err)
	}
	upserted := document.Paths["/upsert"]["put"].Responses
	if upserted["200"].Content["application/json"].Schema == nil || upserted["201"].Content["application/json"].Schema == nil {
		t.Fatal("alternative statuses", upserted)
	}
	redirect := document.Paths["/continue"]["post"].Responses["303"]
	if redirect.Content != nil || redirect.Headers["Location"]["required"] != true {
		t.Fatal("redirect", redirect)
	}
	stream := document.Paths["/events"]["get"].Responses["200"].Content[foundryhttp.EventStreamMediaType]
	if stream.Schema["type"] != "string" || !strings.Contains(string(data), `"x-foundry-event-data"`) {
		t.Fatal("event stream", stream)
	}
	body := document.Paths["/raw"]["post"].RequestBody
	if body == nil || body.Content["application/octet-stream"].Schema["format"] != "binary" || body.Content["image/png"].Schema["format"] != "binary" {
		t.Fatal("raw body", body)
	}
}
