package typescript

import (
	"slices"
	"testing"

	"github.com/weiloon1234/Foundry-Go/contract"
	"github.com/weiloon1234/Foundry-Go/contract/manifest"
	foundryhttp "github.com/weiloon1234/Foundry-Go/http"
	"github.com/weiloon1234/Foundry-Go/i18n"
)

// TestRequestFieldsLabelTree pins the label tree the binder's types follow:
// labelled values, a nested object or a list of objects without a label of
// its own when something below it binds (a list as (entry)[], bound by row),
// a labelled list or map as [entry], a self-referencing object cut where it
// repeats (through an alias too), repeated multipart parts as lists and a JSON
// part as its fields.
func TestRequestFieldsLabelTree(t *testing.T) {
	label := func(key string) contract.Presentation {
		return contract.Presentation{LabelKey: i18n.MessageKey(key)}
	}
	r := renderer{document: manifest.Document{
		Types: []contract.Type{
			{ID: "string", Kind: contract.StringKind},
			{ID: "app.Node", Kind: contract.ObjectKind, Properties: []contract.Property{
				{Name: "name", Type: "string", Required: true, Presentation: label("f.name")},
				{Name: "children", Type: "app.Nodes", Required: true, Presentation: label("f.children")},
				{Name: "hidden", Type: "string", Required: true},
			}},
			{ID: "app.Nodes", Kind: contract.ArrayKind, Element: "app.Node"},
			{ID: "app.Tags", Kind: contract.MapKind, Element: "string"},
			{ID: "app.Wallet", Kind: contract.ObjectKind, Properties: []contract.Property{
				{Name: "address", Type: "string", Required: true, Presentation: label("f.address")},
				{Name: "note", Type: "string", Required: true},
			}},
			{ID: "app.Link", Kind: contract.ObjectKind, Properties: []contract.Property{
				{Name: "role", Type: "string", Required: true, Presentation: label("f.role")},
			}},
			{ID: "app.Links", Kind: contract.ArrayKind, Element: "app.Link"},
			{ID: "app.Codes", Kind: contract.ArrayKind, Element: "string"},
			{ID: "app.Empty", Kind: contract.ObjectKind, Properties: []contract.Property{
				{Name: "hidden", Type: "string", Required: true},
			}},
			{ID: "app.Filter", Kind: contract.ObjectKind, Properties: []contract.Property{
				{Name: "name", Type: "string", Required: true, Presentation: label("f.name")},
				{Name: "not", Type: "app.OptionalFilter", Presentation: label("f.not")},
				{Name: "sub", Type: "app.OptionalFilter"},
			}},
			{ID: "app.OptionalFilter", Kind: contract.AliasKind, Element: "app.Filter"},
			{ID: "app.Body", Kind: contract.ObjectKind, Properties: []contract.Property{
				{Name: "tags", Type: "app.Tags", Required: true, Presentation: label("f.tags")},
				{Name: "root", Type: "app.Node", Required: true, Presentation: label("f.root")},
				{Name: "wallet", Type: "app.Wallet", Required: true},
				{Name: "links", Type: "app.Links", Required: true},
				{Name: "codes", Type: "app.Codes", Required: true},
				{Name: "extra", Type: "app.Tags", Required: true},
				{Name: "empty", Type: "app.Empty", Required: true},
				{Name: "filter", Type: "app.OptionalFilter", Presentation: label("f.filter")},
			}},
			{ID: "app.Meta", Kind: contract.ObjectKind, Properties: []contract.Property{
				{Name: "caption", Type: "string", Required: true, Presentation: label("f.caption")},
			}},
		},
		HTTP: []manifest.Operation{
			{Name: "save", Body: &manifest.Payload{Type: "app.Body"}},
			{Name: "upload", Body: &manifest.Payload{Parts: []manifest.Part{
				{Parameter: manifest.Parameter{Name: "file", Presentation: label("f.file")}, Kind: foundryhttp.MultipartFile},
				{Parameter: manifest.Parameter{Name: "photos", Repeated: true, Presentation: label("f.photos")}, Kind: foundryhttp.MultipartFile},
				{Parameter: manifest.Parameter{Name: "meta", Type: "app.Meta", Presentation: label("f.meta")}, Kind: foundryhttp.MultipartJSON},
				{Parameter: manifest.Parameter{Name: "note", Type: "string"}, Kind: foundryhttp.MultipartText},
				{Parameter: manifest.Parameter{Name: "extra_meta", Type: "app.Meta"}, Kind: foundryhttp.MultipartJSON},
				{Parameter: manifest.Parameter{Name: "scans", Repeated: true}, Kind: foundryhttp.MultipartFile},
			}}},
			{Name: "list", Body: &manifest.Payload{Type: "app.Nodes"}},
		},
	}}
	keys, labelled := r.requestFields()
	want := map[string]string{
		"save": `{ readonly "filter": { readonly "name": true; readonly "not": {}; }; readonly "links": readonly ({ readonly "role": true; })[];` +
			` readonly "root": { readonly "children": readonly [{}]; readonly "name": true; }; readonly "tags": readonly [true]; readonly "wallet": { readonly "address": true; }; }`,
		"upload": `{ readonly "extra_meta": { readonly "caption": true; }; readonly "file": true; readonly "meta": { readonly "caption": true; }; readonly "photos": readonly [true]; }`,
	}
	for name, tree := range want {
		if labelled[name] != tree {
			t.Errorf("%s label tree:\n got %s\nwant %s", name, labelled[name], tree)
		}
	}
	if tree, found := labelled["list"]; found {
		t.Errorf("a list body has no fields to bind, got %s", tree)
	}
	if want := []string{"f.address", "f.caption", "f.children", "f.file", "f.filter", "f.meta", "f.name", "f.not", "f.photos", "f.role", "f.root", "f.tags"}; !slices.Equal(keys, want) {
		t.Errorf("label keys %v, want %v", keys, want)
	}
}
