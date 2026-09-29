package manifest_test

import (
	"context"
	"encoding/json"
	"testing"

	"github.com/weiloon1234/Foundry-Go/contract"
	"github.com/weiloon1234/Foundry-Go/contract/manifest"
	foundryhttp "github.com/weiloon1234/Foundry-Go/http"
)

type noInput = foundryhttp.Input[foundryhttp.NoPath, foundryhttp.NoQuery, foundryhttp.NoBody]

// transportKinds registers one endpoint per newer response/body contract.
func transportKinds(t *testing.T) *manifest.Manifest {
	t.Helper()
	route := func(id foundryhttp.RouteID, method foundryhttp.Method, path string) foundryhttp.Route[foundryhttp.NoPath] {
		return foundryhttp.DefineRoute(foundryhttp.RouteSpec{ID: id, Method: method, Access: foundryhttp.Public}, foundryhttp.StaticPath(path))
	}
	upsert := foundryhttp.DefineEndpoint(route("items.upsert", foundryhttp.PUT, "/upsert"), foundryhttp.EmptyQuery(), foundryhttp.EmptyBody(), foundryhttp.JSONResponses(contract.StringJSON[string](), 200, 201))
	moved := foundryhttp.DefineEndpoint(route("session.continue", foundryhttp.POST, "/continue"), foundryhttp.EmptyQuery(), foundryhttp.EmptyBody(), foundryhttp.RedirectResponse(303))
	events := foundryhttp.DefineEndpoint(route("items.events", foundryhttp.GET, "/events"), foundryhttp.EmptyQuery(), foundryhttp.EmptyBody(), foundryhttp.EventStreamResponse(contract.StringJSON[string]()))
	raw := foundryhttp.DefineEndpoint(route("files.raw", foundryhttp.POST, "/raw"), foundryhttp.EmptyQuery(), foundryhttp.RawRequestBody("application/octet-stream"), foundryhttp.JSONResponse(201, contract.StringJSON[string]()))
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
	return source
}

func TestManifestDescribesStatusesRedirectsAndRawBodies(t *testing.T) {
	document, err := transportKinds(t).Snapshot()
	if err != nil {
		t.Fatal(err)
	}
	operations := make(map[string]manifest.Operation)
	for _, op := range document.HTTP {
		operations[op.Name] = op
	}
	if op := operations["itemsUpsert"]; op.Status != 200 || len(op.Statuses) != 2 || op.Statuses[1] != 201 || op.Response == nil {
		t.Fatal("alternative statuses", op)
	}
	if op := operations["sessionContinue"]; !op.Redirect || op.Status != 303 || op.Response != nil {
		t.Fatal("redirect", op)
	}
	if op := operations["itemsEvents"]; op.Response == nil || op.Response.MediaType != foundryhttp.EventStreamMediaType || op.Response.Type != "string" {
		t.Fatal("event stream", op.Response)
	}
	if op := operations["filesRaw"]; op.Body == nil || op.Body.Raw == nil || op.Body.MediaType != "application/octet-stream" || op.Body.Type != "" || op.Limits.Raw.Bytes <= 0 {
		t.Fatal("raw body", op.Body)
	}
	type mutation struct {
		operation string
		change    func(*manifest.Operation)
	}
	for name, tc := range map[string]mutation{
		"redirect-2xx": {"sessionContinue", func(op *manifest.Operation) { op.Status = 200 }},
		"redirect-with-body": {"sessionContinue", func(op *manifest.Operation) {
			op.Response = &manifest.Payload{MediaType: "application/json", Type: "string"}
		}},
		"redirect-statuses":      {"sessionContinue", func(op *manifest.Operation) { op.Statuses = []int{303, 307} }},
		"status-3xx":             {"sessionContinue", func(op *manifest.Operation) { op.Redirect = false }},
		"statuses-order":         {"itemsUpsert", func(op *manifest.Operation) { op.Statuses = []int{201, 200} }},
		"statuses-bodyless":      {"itemsUpsert", func(op *manifest.Operation) { op.Statuses = []int{200, 204} }},
		"statuses-without-json":  {"itemsUpsert", func(op *manifest.Operation) { op.Response = nil }},
		"raw-with-type":          {"filesRaw", func(op *manifest.Operation) { op.Body.Type = "string" }},
		"raw-without-limit":      {"filesRaw", func(op *manifest.Operation) { op.Limits.Raw.Bytes = 0 }},
		"raw-media-disagreement": {"filesRaw", func(op *manifest.Operation) { op.Body.MediaType = "text/plain" }},
		"events-request": {"filesRaw", func(op *manifest.Operation) {
			op.Body = &manifest.Payload{MediaType: foundryhttp.EventStreamMediaType, Type: "string"}
		}},
		"events-without-type": {"itemsEvents", func(op *manifest.Operation) { op.Response.Type = "" }},
		"raw-response":        {"filesRaw", func(op *manifest.Operation) { op.Response = &manifest.Payload{Raw: op.Body.Raw} }},
	} {
		t.Run(name, func(t *testing.T) {
			changed, err := transportKinds(t).Snapshot()
			if err != nil {
				t.Fatal(err)
			}
			for i := range changed.HTTP {
				if changed.HTTP[i].Name == tc.operation {
					tc.change(&changed.HTTP[i])
				}
			}
			data, err := json.Marshal(changed)
			if err != nil {
				t.Fatal(err)
			}
			if _, err := manifest.Decode(data); err == nil {
				t.Fatal("invalid transport contract accepted")
			}
		})
	}
}
