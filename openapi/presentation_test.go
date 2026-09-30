package openapi_test

import (
	"context"
	"encoding/json"
	"testing"

	"github.com/weiloon1234/Foundry-Go/contract"
	"github.com/weiloon1234/Foundry-Go/contract/manifest"
	foundryhttp "github.com/weiloon1234/Foundry-Go/http"
	"github.com/weiloon1234/Foundry-Go/openapi"
)

// Hints reach every transport input, and a repeated input's hint describes
// each element, as in the manifest.
func TestOpenAPIPresentsURLFormAndMultipartInputs(t *testing.T) {
	type Path struct{ Slug string }
	type Search struct{ Sites []string }
	type Login struct{ Secret string }
	type Upload struct {
		Note  string
		Photo foundryhttp.UploadedFile
	}
	route := func(id foundryhttp.RouteID, method foundryhttp.Method, pattern string) foundryhttp.Route[foundryhttp.NoPath] {
		return foundryhttp.DefineRoute(foundryhttp.RouteSpec{ID: id, Method: method, Access: foundryhttp.Public}, foundryhttp.StaticPath(pattern))
	}
	text := contract.Presentation{Kind: contract.TextPresentation, LabelKey: "fields.slug"}
	show := foundryhttp.DefineEndpoint(
		foundryhttp.DefineRoute(foundryhttp.RouteSpec{ID: "items.show", Method: foundryhttp.GET, Access: foundryhttp.Public}, foundryhttp.DefinePath("/items/{slug}", foundryhttp.Param("slug", foundryhttp.StringPath[string](), func(p *Path) *string { return &p.Slug }).WithPresentation(text))),
		foundryhttp.DefineQuery(foundryhttp.RepeatedQueryParam("site", foundryhttp.StringQuery[string](), func(q *Search) *[]string { return &q.Sites }).WithPresentation(contract.Presentation{Kind: contract.URLPresentation})),
		foundryhttp.EmptyBody(), foundryhttp.JSONResponse(200, contract.StringJSON[string]()))
	login := foundryhttp.DefineEndpoint(route("session.create", foundryhttp.POST, "/session"), foundryhttp.EmptyQuery(),
		foundryhttp.FormBody(foundryhttp.DefineQuery(foundryhttp.QueryParam("secret", foundryhttp.StringQuery[string](), func(l *Login) *string { return &l.Secret }).WithPresentation(contract.Presentation{Kind: contract.PasswordPresentation}))),
		foundryhttp.JSONResponse(200, contract.StringJSON[string]()))
	upload := foundryhttp.DefineEndpoint(route("uploads.create", foundryhttp.POST, "/uploads"), foundryhttp.EmptyQuery(),
		foundryhttp.MultipartBody(foundryhttp.DefineMultipart(
			foundryhttp.TextPart(foundryhttp.QueryParam("note", foundryhttp.StringQuery[string](), func(u *Upload) *string { return &u.Note })).WithPresentation(contract.Presentation{Kind: contract.MultilinePresentation}),
			foundryhttp.FilePart("photo", func(u *Upload) *foundryhttp.UploadedFile { return &u.Photo }).WithPresentation(contract.Presentation{Kind: contract.FilePresentation}),
		)),
		foundryhttp.JSONResponse(201, contract.StringJSON[string]()))
	router, err := foundryhttp.NewRouter(
		show.Handle(func(context.Context, foundryhttp.Input[Path, Search, foundryhttp.NoBody]) (string, error) {
			return "", nil
		}),
		login.Handle(func(context.Context, foundryhttp.Input[foundryhttp.NoPath, foundryhttp.NoQuery, Login]) (string, error) {
			return "", nil
		}),
		upload.Handle(func(context.Context, foundryhttp.Input[foundryhttp.NoPath, foundryhttp.NoQuery, Upload]) (string, error) {
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
	data, err := openapi.Render(source, openapi.Options{Title: "Presentation", APIVersion: "1"})
	if err != nil {
		t.Fatal(err)
	}
	var document map[string]any
	if err := json.Unmarshal(data, &document); err != nil {
		t.Fatal(err)
	}
	at := func(value any, keys ...string) map[string]any {
		t.Helper()
		for _, key := range keys {
			object, ok := value.(map[string]any)
			if !ok {
				t.Fatalf("missing %s", key)
			}
			value = object[key]
		}
		object, ok := value.(map[string]any)
		if !ok {
			t.Fatalf("missing %v", keys)
		}
		return object
	}
	kind := func(schema map[string]any) any {
		hint, _ := schema["x-foundry-presentation"].(map[string]any)
		return hint["kind"]
	}
	parameters := map[string]map[string]any{}
	for _, raw := range at(document, "paths", "/items/{slug}", "get")["parameters"].([]any) {
		parameter := raw.(map[string]any)
		parameters[parameter["name"].(string)] = parameter["schema"].(map[string]any)
	}
	if kind(parameters["slug"]) != "text" {
		t.Fatal("path parameter presentation missing", parameters["slug"])
	}
	if site := parameters["site"]; site["type"] != "array" || site["x-foundry-presentation"] != nil || kind(at(site, "items")) != "url" {
		t.Fatal("repeated query presentation must describe each element", site)
	}
	secret := at(document, "paths", "/session", "post", "requestBody", "content", "application/x-www-form-urlencoded", "schema", "properties", "secret")
	if kind(secret) != "password" || secret["writeOnly"] != true {
		t.Fatal("form field presentation missing", secret)
	}
	parts := at(document, "paths", "/uploads", "post", "requestBody", "content", "multipart/form-data", "schema", "properties")
	if kind(at(parts, "note")) != "multiline" || kind(at(parts, "photo")) != "file" {
		t.Fatal("multipart presentation missing", parts)
	}
}
