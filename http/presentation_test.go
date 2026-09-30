package http_test

import (
	"context"
	"reflect"
	"strings"
	"testing"

	"github.com/weiloon1234/Foundry-Go/contract"
	foundryhttp "github.com/weiloon1234/Foundry-Go/http"
)

func TestTransportPresentationUsesActualCodec(t *testing.T) {
	type Input struct {
		Name  string
		Count int
		File  foundryhttp.UploadedFile
	}
	text := foundryhttp.QueryParam("name", foundryhttp.StringQuery[string](), func(i *Input) *string { return &i.Name })
	hint := contract.Presentation{Kind: contract.EmailPresentation, LabelKey: "fields.email"}
	query := foundryhttp.DefineQuery(text.WithPresentation(hint))
	fields, err := query.Parameters()
	if err != nil || fields[0].Presentation != hint {
		t.Fatal(fields, err)
	}
	fields[0].Presentation.Kind = contract.FilePresentation
	fields, err = query.Parameters()
	if err != nil || fields[0].Presentation != hint {
		t.Fatal("metadata alias", err)
	}
	if foundryhttp.DefineQuery(text.WithPresentation(contract.Presentation{Kind: contract.MoneyPresentation})).Validate() == nil {
		t.Fatal("money on plain text accepted")
	}
	path := foundryhttp.DefinePath("/{name}", foundryhttp.Param("name", foundryhttp.StringPath[string](), func(i *Input) *string { return &i.Name }).WithPresentation(hint))
	paths, err := path.Parameters()
	if err != nil || paths[0].Presentation != hint {
		t.Fatal(paths, err)
	}
	badPath := foundryhttp.DefinePath("/{count}", foundryhttp.Param("count", foundryhttp.IntegerPath[int](), func(i *Input) *int { return &i.Count }).WithPresentation(hint))
	if _, err := badPath.Parameters(); err == nil {
		t.Fatal("email on integer path accepted")
	}
	file := foundryhttp.FilePart("file", func(i *Input) *foundryhttp.UploadedFile { return &i.File })
	parts := foundryhttp.DefineMultipart(foundryhttp.TextPart(text).WithPresentation(hint), file.WithPresentation(contract.Presentation{Kind: contract.FilePresentation}))
	description, err := parts.Description()
	if err != nil || len(description.Parts) != 2 {
		t.Fatal(description, err)
	}
	if err := foundryhttp.DefineMultipart(file.WithPresentation(hint)).Validate(); err == nil || !strings.Contains(err.Error(), `"file"`) {
		t.Fatal("email file accepted or not named", err)
	}
	type Parts struct{ Count int }
	meta := foundryhttp.JSONPart("meta", contract.IntegerJSON[int](), func(p *Parts) *int { return &p.Count })
	if err := foundryhttp.DefineMultipart(meta.WithPresentation(contract.Presentation{Kind: contract.TextPresentation})).Validate(); err == nil || !strings.Contains(err.Error(), `"meta"`) {
		t.Fatal("contradictory JSON part accepted or not named", err)
	}
}

func TestPresentationKeepsCredentialsOutOfURLsAndOutputs(t *testing.T) {
	type Input struct {
		Token string
		Count int
	}
	password := contract.Presentation{Kind: contract.PasswordPresentation}
	// Path segments are URL components; a password hint never describes one.
	path := foundryhttp.DefinePath("/{token}", foundryhttp.Param("token", foundryhttp.StringPath[string](), func(i *Input) *string { return &i.Token }).WithPresentation(password))
	if _, err := path.Parameters(); err == nil {
		t.Fatal("password path parameter accepted")
	}
	route := func(id foundryhttp.RouteID, method foundryhttp.Method, pattern string) foundryhttp.Route[foundryhttp.NoPath] {
		return foundryhttp.DefineRoute(foundryhttp.RouteSpec{ID: id, Method: method, Access: foundryhttp.Public}, foundryhttp.StaticPath(pattern))
	}
	// One declaration is a URL query on one endpoint and a form body on another.
	fields := foundryhttp.DefineQuery(foundryhttp.QueryParam("token", foundryhttp.StringQuery[string](), func(i *Input) *string { return &i.Token }).WithPresentation(password))
	if foundryhttp.DefineEndpoint(route("tokens.show", foundryhttp.GET, "/tokens"), fields, foundryhttp.EmptyBody(), foundryhttp.JSONResponse(200, contract.StringJSON[string]())).Validate() == nil {
		t.Fatal("password query parameter accepted")
	}
	if err := foundryhttp.DefineEndpoint(route("tokens.create", foundryhttp.POST, "/tokens"), foundryhttp.EmptyQuery(), foundryhttp.FormBody(fields), foundryhttp.JSONResponse(200, contract.StringJSON[string]())).Validate(); err != nil {
		t.Fatal("password form field rejected", err)
	}
	// Output graphs reaching a password hint fail registration, even with no
	// client export; the declaration itself remains a valid input DTO.
	type Secret struct {
		Password string `json:"password"`
	}
	id := contract.TypeID(reflect.TypeFor[Secret]().PkgPath() + ".Secret")
	secret := contract.DefineJSON[Secret](contract.Schema{Root: id, Types: []contract.Type{
		{ID: id, Kind: contract.ObjectKind, Properties: []contract.Property{{Name: "password", Type: "string", Required: true, Presentation: password}}},
		{ID: "string", Kind: contract.StringKind},
	}})
	type none = foundryhttp.Input[foundryhttp.NoPath, foundryhttp.NoQuery, foundryhttp.NoBody]
	echo := foundryhttp.DefineEndpoint(route("secrets.show", foundryhttp.GET, "/secret"), foundryhttp.EmptyQuery(), foundryhttp.EmptyBody(), foundryhttp.JSONResponse(200, secret))
	if _, err := foundryhttp.NewRouter(echo.Handle(func(context.Context, none) (Secret, error) { return Secret{}, nil })); err == nil {
		t.Fatal("password response registered")
	}
	events := foundryhttp.DefineEndpoint(route("secrets.events", foundryhttp.GET, "/secrets"), foundryhttp.EmptyQuery(), foundryhttp.EmptyBody(), foundryhttp.EventStreamResponse(secret))
	if _, err := foundryhttp.NewRouter(events.Handle(func(context.Context, none) (foundryhttp.Events[Secret], error) {
		return foundryhttp.EventsFrom(func(context.Context, *foundryhttp.EventSink[Secret]) error { return nil }), nil
	})); err == nil {
		t.Fatal("password event stream registered")
	}
	accepted := foundryhttp.DefineEndpoint(route("secrets.create", foundryhttp.POST, "/secrets"), foundryhttp.EmptyQuery(), foundryhttp.JSONBody(secret), foundryhttp.JSONResponse(201, contract.StringJSON[string]()))
	if _, err := foundryhttp.NewRouter(accepted.Handle(func(context.Context, foundryhttp.Input[foundryhttp.NoPath, foundryhttp.NoQuery, Secret]) (string, error) {
		return "", nil
	})); err != nil {
		t.Fatal("password request body rejected", err)
	}
	// Registration names a contradicting parameter.
	count := foundryhttp.DefineQuery(foundryhttp.QueryParam("count", foundryhttp.IntegerQuery[int](), func(i *Input) *int { return &i.Count }).WithPresentation(contract.Presentation{Kind: contract.EmailPresentation}))
	if err := count.Validate(); err == nil || !strings.Contains(err.Error(), `"count"`) {
		t.Fatal("contradiction did not name its parameter", err)
	}
}
