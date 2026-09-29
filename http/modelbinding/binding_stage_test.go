package modelbinding_test

import (
	"context"
	"net/http/httptest"
	"reflect"
	"strings"
	"sync/atomic"
	"testing"

	"github.com/weiloon1234/Foundry-Go/contract"
	foundryhttp "github.com/weiloon1234/Foundry-Go/http"
	"github.com/weiloon1234/Foundry-Go/http/modelbinding"
	"github.com/weiloon1234/Foundry-Go/validation"
	"github.com/weiloon1234/Foundry-Go/value"
)

type Rename struct {
	Name string `json:"name"`
}
type renameInput = modelbinding.Input[path, foundryhttp.NoQuery, Rename, member]

func renameEndpoint() foundryhttp.Endpoint[path, foundryhttp.NoQuery, Rename, foundryhttp.NoContent] {
	root := contract.TypeID(reflect.TypeFor[Rename]().PkgPath() + ".Rename")
	body := contract.DefineJSON[Rename](contract.Schema{Root: root, Types: []contract.Type{
		{ID: root, Kind: contract.ObjectKind, Properties: []contract.Property{{Name: "name", Type: "string", Required: true}}},
		{ID: "string", Kind: contract.StringKind},
	}})
	return foundryhttp.DefineEndpoint(
		foundryhttp.DefineRoute(foundryhttp.RouteSpec{ID: "members.rename", Method: foundryhttp.PATCH, Access: foundryhttp.Public},
			foundryhttp.DefinePath("/members/{member}", foundryhttp.Param("member", foundryhttp.IntegerPath[memberKey](), func(p *path) *memberKey { return &p.Member }))),
		foundryhttp.EmptyQuery(), foundryhttp.JSONBody(body), foundryhttp.EmptyResponse(204),
	).WithBodyValidation(validation.DefineField("name", func(b Rename) string { return b.Name }).Rules(validation.NonBlank[string]()))
}

// A missing bound model is 404 and a resource denial 403 before body
// validation can report 422; the handler receives the model from the single
// lookup performed by the binding stage.
func TestBindingStageResolvesBeforeBodyValidation(t *testing.T) {
	var lookups, handled atomic.Int32
	resolver := modelbinding.Define(func(_ context.Context, p path) (value.Optional[member], error) {
		lookups.Add(1)
		if p.Member == 404 {
			return value.Optional[member]{}, nil
		}
		return value.Set(member{ID: p.Member}), nil
	})
	bound := modelbinding.Bind(renameEndpoint(), resolver).WithAuthorization(func(_ context.Context, in renameInput) error {
		if in.Model.ID == 403 {
			return foundryhttp.Forbidden
		}
		return nil
	})
	r := router(t, bound.Handle(func(_ context.Context, in renameInput) (foundryhttp.NoContent, error) {
		handled.Add(1)
		if in.Model.ID != 42 || in.Request.Body.Name != "Jane" {
			t.Error("handler lost the bound model or request")
		}
		return foundryhttp.NoContent{}, nil
	}))
	for _, tc := range []struct {
		member, body string
		status       int
	}{{"404", `{"name":" "}`, 404}, {"403", `{"name":" "}`, 403}, {"42", `{"name":" "}`, 422}, {"42", `{"name":"Jane"}`, 204}} {
		before := lookups.Load()
		request := httptest.NewRequest("PATCH", "/members/"+tc.member, strings.NewReader(tc.body))
		request.Header.Set("Content-Type", "application/json")
		w := httptest.NewRecorder()
		r.ServeHTTP(w, request)
		if w.Code != tc.status || lookups.Load()-before != 1 {
			t.Fatalf("%s %s: %d after %d lookups %s", tc.member, tc.body, w.Code, lookups.Load()-before, w.Body.String())
		}
	}
	if handled.Load() != 1 {
		t.Fatal("handler count", handled.Load())
	}
}

// customTransport lacks the binding capability; resolution keeps its
// handler-position fallback after validation.
type customTransport struct {
	foundryhttp.Endpoint[path, foundryhttp.NoQuery, Rename, foundryhttp.NoContent]
}

func (c customTransport) Handle(handler foundryhttp.Handler[path, foundryhttp.NoQuery, Rename, foundryhttp.NoContent]) foundryhttp.RouteRegistration {
	return c.Endpoint.Handle(handler)
}
func (c customTransport) HandleBound() {}

func TestBindingWithoutStageFallsBackAfterValidation(t *testing.T) {
	var lookups atomic.Int32
	resolver := modelbinding.Define(func(context.Context, path) (value.Optional[member], error) {
		lookups.Add(1)
		return value.Optional[member]{}, nil
	})
	r := router(t, modelbinding.Bind[path, foundryhttp.NoQuery, Rename, member, foundryhttp.NoContent](customTransport{renameEndpoint()}, resolver).Handle(func(context.Context, renameInput) (foundryhttp.NoContent, error) {
		return foundryhttp.NoContent{}, nil
	}))
	request := httptest.NewRequest("PATCH", "/members/404", strings.NewReader(`{"name":" "}`))
	request.Header.Set("Content-Type", "application/json")
	w := httptest.NewRecorder()
	r.ServeHTTP(w, request)
	if w.Code != 422 || lookups.Load() != 0 {
		t.Fatal("custom transport fallback", w.Code, lookups.Load())
	}
}
