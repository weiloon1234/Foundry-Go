package manifest_test

import (
	"bytes"
	"context"
	"encoding/json"
	"reflect"
	"strings"
	"testing"

	"github.com/weiloon1234/Foundry-Go/contract"
	"github.com/weiloon1234/Foundry-Go/contract/manifest"
	foundryhttp "github.com/weiloon1234/Foundry-Go/http"
	"github.com/weiloon1234/Foundry-Go/i18n"
	"github.com/weiloon1234/Foundry-Go/openapi"
	"github.com/weiloon1234/Foundry-Go/typescript"
	"github.com/weiloon1234/Foundry-Go/validation"
)

func credentialManifest(t testing.TB) *manifest.Manifest {
	t.Helper()
	type Secret struct {
		Password string `json:"password"`
	}
	type Input struct {
		Children []Secret `json:"children"`
	}
	inputID := contract.TypeID(reflect.TypeFor[Input]().PkgPath() + ".Input")
	wire := contract.DefineJSON[Input](contract.Schema{Root: inputID, Types: []contract.Type{
		{ID: inputID, Kind: contract.ObjectKind, Properties: []contract.Property{{Name: "children", Type: "Children", Required: true}}},
		{ID: "Children", Kind: contract.ArrayKind, Element: "Secret"},
		{ID: "Secret", Kind: contract.ObjectKind, Properties: []contract.Property{{Name: "password", Type: "string", Required: true, Presentation: contract.Presentation{Kind: contract.PasswordPresentation, LabelKey: "fields.password"}}}},
		{ID: "string", Kind: contract.StringKind},
	}})
	ep := foundryhttp.DefineEndpoint(foundryhttp.DefineRoute(foundryhttp.RouteSpec{ID: "credentials.create", Method: foundryhttp.POST, Access: foundryhttp.Public}, foundryhttp.StaticPath("/credentials")), foundryhttp.EmptyQuery(), foundryhttp.JSONBody(wire), foundryhttp.JSONResponse(200, contract.StringJSON[string]()))
	router, err := foundryhttp.NewRouter(ep.Handle(func(context.Context, foundryhttp.Input[foundryhttp.NoPath, foundryhttp.NoQuery, Input]) (string, error) {
		return "ok", nil
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

func TestPresentationRejectsConflictingNestedValidation(t *testing.T) {
	source := credentialManifest(t)
	email, err := validation.Email[string]().Description()
	if err != nil {
		t.Fatal(err)
	}
	url, err := validation.URL[string]().Description()
	if err != nil {
		t.Fatal(err)
	}
	field := func(name string, child validation.Description) validation.Description {
		return validation.Description{Kind: validation.FieldKind, Field: name, Children: []validation.Description{child}}
	}
	for _, test := range []struct {
		name    string
		label   i18n.MessageKey
		rule    validation.Description
		unknown bool
		valid   bool
	}{
		{"matching", "fields.email", email, false, true},
		{"label conflict", "fields.other", email, false, false},
		{"format conflict", "fields.email", url, false, false},
		{"unknown selector stays unknown", "fields.other", email, true, true},
	} {
		t.Run(test.name, func(t *testing.T) {
			doc, _ := source.Snapshot()
			for i := range doc.Types {
				if doc.Types[i].ID == "Secret" {
					doc.Types[i].Properties[0].Presentation = contract.Presentation{Kind: contract.EmailPresentation, LabelKey: "fields.email"}
				}
			}
			leaf := field("password", test.rule)
			leaf.LabelKey = test.label
			rule := field("body", field("children", validation.Description{Kind: validation.EachKind, Children: []validation.Description{leaf}}))
			if test.unknown {
				rule = field("unknown", rule)
			}
			doc.HTTP[0].Validation = &rule
			raw, err := json.Marshal(doc)
			if err != nil {
				t.Fatal(err)
			}
			_, err = manifest.Decode(raw)
			if (err == nil) != test.valid {
				t.Fatalf("valid=%v, error=%v", test.valid, err)
			}
		})
	}
}

func TestPresentationManifestVersionPrivacyAndAdapters(t *testing.T) {
	source := credentialManifest(t)
	doc, _ := source.Snapshot()
	if doc.Version != 6 {
		t.Fatal("new metadata needs explicit manifest version")
	}
	for name, edit := range map[string]func(*manifest.Document){
		"old format":      func(d *manifest.Document) { d.Version = 5 },
		"nested response": func(d *manifest.Document) { d.HTTP[0].Response.Type = d.HTTP[0].Body.Type },
		"nested example": func(d *manifest.Document) {
			d.HTTP[0].Body.Example = json.RawMessage(`{"children":[{"password":"credential-canary"}]}`)
		},
		"unknown hint": func(d *manifest.Document) {
			for i := range d.Types {
				if d.Types[i].ID == "Secret" {
					d.Types[i].Properties[0].Presentation.Kind = "script"
				}
			}
		},
	} {
		t.Run(name, func(t *testing.T) {
			d, _ := source.Snapshot()
			edit(&d)
			raw, _ := json.Marshal(d)
			if _, err := manifest.Decode(raw); err == nil {
				t.Fatal("invalid public metadata accepted")
			}
		})
	}
	raw, _ := source.JSON()
	malformed := bytes.Replace(raw, []byte(`"kind": "password"`), []byte(`"kind": "password", "default": "credential-canary"`), 1)
	if _, err := manifest.Decode(malformed); err == nil {
		t.Fatal("arbitrary hint values accepted")
	}
	api, err := openapi.Render(source, openapi.Options{Title: "Credentials", APIVersion: "1"})
	if err != nil {
		t.Fatal(err)
	}
	ts, err := typescript.Render(source)
	if err != nil {
		t.Fatal(err)
	}
	for _, data := range [][]byte{raw, api, ts} {
		if bytes.Contains(data, []byte("credential-canary")) {
			t.Fatal("private example leaked")
		}
		if !bytes.Contains(data, []byte("fields.password")) {
			t.Fatal("presentation lost")
		}
	}
	if !strings.Contains(string(api), `"writeOnly": true`) {
		t.Fatal("password field not marked input-only")
	}
}

func TestPresentationKeepsURLCredentialsOutAndChecksElements(t *testing.T) {
	source := credentialManifest(t)
	email, err := validation.Email[string]().Description()
	if err != nil {
		t.Fatal(err)
	}
	url, err := validation.URL[string]().Description()
	if err != nil {
		t.Fatal(err)
	}
	same, err := validation.Same[string]().Description()
	if err != nil {
		t.Fatal(err)
	}
	field := func(name string, child validation.Description) validation.Description {
		return validation.Description{Kind: validation.FieldKind, Field: name, Children: []validation.Description{child}}
	}
	each := func(child validation.Description) validation.Description {
		return validation.Description{Kind: validation.EachKind, Children: []validation.Description{child}}
	}
	decode := func(edit func(*manifest.Document)) error {
		d, _ := source.Snapshot()
		edit(&d)
		raw, err := json.Marshal(d)
		if err != nil {
			t.Fatal(err)
		}
		_, err = manifest.Decode(raw)
		return err
	}
	// Credentials never travel in URLs, whatever produced the manifest.
	if decode(func(d *manifest.Document) {
		d.HTTP[0].Query = []manifest.Parameter{{Name: "token", Type: "string", Syntax: foundryhttp.TextURLSyntax, Presentation: contract.Presentation{Kind: contract.PasswordPresentation}}}
	}) == nil {
		t.Fatal("password query parameter accepted")
	}
	// A repeated parameter's hint describes each element, so its element rule
	// is checked against that hint rather than skipped.
	site := manifest.Parameter{Name: "site", Type: "string", Syntax: foundryhttp.TextURLSyntax, Repeated: true, Presentation: contract.Presentation{Kind: contract.URLPresentation}}
	for name, test := range map[string]struct {
		rule  validation.Description
		valid bool
	}{"matching element rule": {url, true}, "conflicting element rule": {email, false}} {
		t.Run(name, func(t *testing.T) {
			err := decode(func(d *manifest.Document) {
				d.HTTP[0].Query = []manifest.Parameter{site}
				rule := field("query", field("site", each(test.rule)))
				d.HTTP[0].Validation = &rule
			})
			if (err == nil) != test.valid {
				t.Fatalf("valid=%v, error=%v", test.valid, err)
			}
		})
	}
	// A comparison's other label is checked against that sibling's presentation.
	for name, test := range map[string]struct {
		other i18n.MessageKey
		valid bool
	}{"matching other label": {"fields.confirm", true}, "conflicting other label": {"fields.other", false}} {
		t.Run(name, func(t *testing.T) {
			err := decode(func(d *manifest.Document) {
				for i := range d.Types {
					if d.Types[i].ID == "Secret" {
						confirm := contract.Property{Name: "confirm", Type: "string", Required: true, Presentation: contract.Presentation{LabelKey: "fields.confirm"}}
						d.Types[i].Properties = append([]contract.Property{confirm}, d.Types[i].Properties...)
					}
				}
				compare := validation.Description{Kind: validation.CompareKind, Field: "password", OtherField: "confirm", LabelKey: "fields.password", OtherLabelKey: test.other, Children: []validation.Description{same}}
				rule := field("body", field("children", each(compare)))
				d.HTTP[0].Validation = &rule
			})
			if (err == nil) != test.valid {
				t.Fatalf("valid=%v, error=%v", test.valid, err)
			}
		})
	}
}
