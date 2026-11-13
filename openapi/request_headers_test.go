package openapi_test

import (
	"context"
	"encoding/json"
	"testing"

	"github.com/weiloon1234/Foundry-Go/contract"
	"github.com/weiloon1234/Foundry-Go/contract/manifest"
	foundryhttp "github.com/weiloon1234/Foundry-Go/http"
	"github.com/weiloon1234/Foundry-Go/openapi"
	"github.com/weiloon1234/Foundry-Go/value"
)

func TestOpenAPIRequestHeaders(t *testing.T) {
	type input struct {
		Version  string
		Sequence value.Optional[int64]
	}
	endpoint := foundryhttp.DefineEndpoint(foundryhttp.DefineRoute(foundryhttp.RouteSpec{ID: "headers", Method: foundryhttp.GET, Access: foundryhttp.Public}, foundryhttp.StaticPath("/headers")), foundryhttp.DefineQuery(
		foundryhttp.HeaderParam("x-app-version", foundryhttp.StringQuery[string](), func(q *input) *string { return &q.Version }).WithPresentation(contract.Presentation{Kind: contract.TextPresentation, LabelKey: "fields.version"}),
		foundryhttp.OptionalHeaderParam("x-sequence", foundryhttp.IntegerQuery[int64](), func(q *input) *value.Optional[int64] { return &q.Sequence }),
	), foundryhttp.EmptyBody(), foundryhttp.EmptyResponse(204))
	router, err := foundryhttp.NewRouter(endpoint.Handle(func(context.Context, foundryhttp.Input[foundryhttp.NoPath, input, foundryhttp.NoBody]) (foundryhttp.NoContent, error) {
		return foundryhttp.NoContent{}, nil
	}))
	if err != nil {
		t.Fatal(err)
	}
	source, err := manifest.Build(t.Context(), manifest.Sources{HTTP: router})
	if err != nil {
		t.Fatal(err)
	}
	data, err := openapi.Render(source, openapi.Options{Title: "Headers", APIVersion: "1"})
	if err != nil {
		t.Fatal(err)
	}
	var document struct {
		Paths map[string]map[string]struct {
			Parameters []struct {
				Name     string         `json:"name"`
				In       string         `json:"in"`
				Required bool           `json:"required"`
				Style    string         `json:"style"`
				Explode  *bool          `json:"explode"`
				Schema   map[string]any `json:"schema"`
			} `json:"parameters"`
		} `json:"paths"`
	}
	if err := json.Unmarshal(data, &document); err != nil {
		t.Fatal(err)
	}
	parameters := document.Paths["/headers"]["get"].Parameters
	if len(parameters) != 2 {
		t.Fatal(parameters)
	}
	for i, parameter := range parameters {
		if parameter.In != "header" || parameter.Style != "simple" || parameter.Explode == nil || *parameter.Explode || parameter.Required != (i == 0) || parameter.Schema["$ref"] == nil || parameter.Schema["type"] == "array" {
			t.Fatal(parameter)
		}
	}
	if parameters[0].Name != "X-App-Version" || parameters[1].Name != "X-Sequence" {
		t.Fatal(parameters)
	}
}
