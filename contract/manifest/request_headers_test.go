package manifest_test

import (
	"bytes"
	"context"
	"encoding/json"
	"reflect"
	"slices"
	"testing"

	"github.com/weiloon1234/Foundry-Go/contract"
	"github.com/weiloon1234/Foundry-Go/contract/manifest"
	foundryhttp "github.com/weiloon1234/Foundry-Go/http"
	"github.com/weiloon1234/Foundry-Go/validation"
	"github.com/weiloon1234/Foundry-Go/value"
)

type headerVersion string
type headerInput struct {
	Version  headerVersion
	Sequence value.Optional[int64]
}

func headerManifest(t *testing.T) *manifest.Manifest {
	t.Helper()
	parameters := foundryhttp.DefineQuery(
		foundryhttp.OptionalHeaderParam("x-sequence", foundryhttp.IntegerQuery[int64](), func(q *headerInput) *value.Optional[int64] { return &q.Sequence }),
		foundryhttp.HeaderParam("x-app-version", foundryhttp.StringQuery[headerVersion](), func(q *headerInput) *headerVersion { return &q.Version }).WithPresentation(contract.Presentation{Kind: contract.TextPresentation, LabelKey: "fields.version"}),
	)
	d := foundryhttp.DefineEndpoint(foundryhttp.DefineRoute(foundryhttp.RouteSpec{ID: "headers", Method: foundryhttp.GET, Access: foundryhttp.Public}, foundryhttp.StaticPath("/headers")), parameters, foundryhttp.EmptyBody(), foundryhttp.EmptyResponse(204))
	router, err := foundryhttp.NewRouter(endpoint(t, "plain", "/plain"), d.Handle(func(context.Context, foundryhttp.Input[foundryhttp.NoPath, headerInput, foundryhttp.NoBody]) (foundryhttp.NoContent, error) {
		t.Fatal("metadata invoked handler")
		return foundryhttp.NoContent{}, nil
	}))
	if err != nil {
		t.Fatal(err)
	}
	source, err := manifest.Build(t.Context(), manifest.Sources{HTTP: router})
	if err != nil {
		t.Fatal(err)
	}
	return source
}

func TestRequestHeaderManifestNormalizationAndProjection(t *testing.T) {
	source := headerManifest(t)
	original, _ := source.JSON()
	doc, _ := source.Snapshot()
	if doc.Version != 11 || len(doc.HTTP[0].Query) != 0 || len(doc.HTTP[0].Headers) != 2 || doc.HTTP[1].Headers == nil {
		t.Fatal("missing separate v11 header collection")
	}
	parameters := doc.HTTP[0].Headers
	if parameters[0].Name != "X-App-Version" || !parameters[0].Required || parameters[1].Name != "X-Sequence" || parameters[1].Required {
		t.Fatal(parameters)
	}
	for i := range doc.HTTP[0].Headers {
		doc.HTTP[0].Headers[i].Name = []string{"x-app-version", "X-SEQUENCE"}[i]
	}
	slices.Reverse(doc.HTTP[0].Headers)
	doc.HTTP[1].Headers = nil
	wire, _ := json.Marshal(doc)
	normalized, err := manifest.Decode(wire)
	if err != nil {
		t.Fatal(err)
	}
	again, _ := normalized.JSON()
	if !bytes.Equal(original, again) {
		t.Fatal("headers did not normalize canonically")
	}
	selected := project(t, source, manifest.Selection{Routes: []foundryhttp.RouteID{"headers"}})
	if !slices.ContainsFunc(selected.Types, func(typ contract.Type) bool { return typ.ID == parameters[0].Type }) {
		t.Fatal("projection lost header-only type")
	}
	plain := project(t, source, manifest.Selection{Routes: []foundryhttp.RouteID{"plain"}})
	if slices.ContainsFunc(plain.Types, func(typ contract.Type) bool { return typ.ID == parameters[0].Type }) {
		t.Fatal("unselected header type survived projection")
	}
	if !reflect.DeepEqual(plain.HTTP[0], doc.HTTP[1]) {
		// Missing collections normalize to arrays; all other operation metadata stays intact.
		doc.HTTP[1].Headers = []manifest.Parameter{}
		if !reflect.DeepEqual(plain.HTTP[0], doc.HTTP[1]) {
			t.Fatal("header-free operation changed")
		}
	}
	parameters[0].Presentation.LabelKey = "changed"
	stable, _ := source.JSON()
	if !bytes.Equal(original, stable) {
		t.Fatal("header snapshot is not owned")
	}
}

func TestRequestHeaderManifestRejectsInvalidDeclarations(t *testing.T) {
	source := headerManifest(t)
	for name, change := range map[string]func(*manifest.Document){
		"v10":             func(d *manifest.Document) { d.Version = 10 },
		"reserved":        func(d *manifest.Document) { d.HTTP[0].Headers[0].Name = "aUtHoRiZaTiOn" },
		"reserved prefix": func(d *manifest.Document) { d.HTTP[0].Headers[0].Name = "X-Forwarded-Custom" },
		"idempotency":     func(d *manifest.Document) { d.HTTP[0].Headers[0].Name = "Idempotency-Key" },
		"duplicate":       func(d *manifest.Document) { d.HTTP[0].Headers[1].Name = "x-app-VERSION" },
		"name":            func(d *manifest.Document) { d.HTTP[0].Headers[0].Name = "X App" },
		"repeated":        func(d *manifest.Document) { d.HTTP[0].Headers[0].Repeated = true },
		"catch all":       func(d *manifest.Document) { d.HTTP[0].Headers[0].CatchAll = true },
		"default":         func(d *manifest.Document) { d.HTTP[0].Headers[1].DefaultURL = value.Set("1") },
		"reference":       func(d *manifest.Document) { d.HTTP[0].Headers[0].Type = "missing" },
		"object":          func(d *manifest.Document) { d.HTTP[0].Headers[0].Type = d.ErrorType },
		"syntax":          func(d *manifest.Document) { d.HTTP[0].Headers[0].Syntax = foundryhttp.IntegerURLSyntax },
		"inactive limits": func(d *manifest.Document) { d.HTTP[0].Limits.Headers = foundryhttp.RequestHeaderLimits{} },
		"field limit":     func(d *manifest.Document) { d.HTTP[0].Limits.Headers.Fields = 1 },
		"byte limit": func(d *manifest.Document) {
			d.HTTP[0].Limits.Headers.FieldBytes = 1
			d.HTTP[0].Limits.Headers.Bytes = 2
		},
		"label": func(d *manifest.Document) {
			rule, err := validation.DescribeRule(validation.Rules(validation.DefineField("headers", func(q headerInput) headerInput { return q }), validation.Rules(validation.WithLabelKey(validation.DefineField("X-App-Version", func(q headerInput) headerVersion { return q.Version }), "fields.other"), validation.NonBlank[headerVersion]())))
			if err != nil {
				t.Fatal(err)
			}
			d.HTTP[0].Validation = &rule
		},
	} {
		t.Run(name, func(t *testing.T) {
			doc, _ := source.Snapshot()
			change(&doc)
			data, err := json.Marshal(doc)
			if err != nil {
				t.Fatal(err)
			}
			if _, err := manifest.Decode(data); err == nil {
				t.Fatal("invalid header manifest accepted")
			}
		})
	}
}
