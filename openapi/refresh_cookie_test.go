package openapi_test

import (
	"context"
	"encoding/json"
	"strings"
	"testing"

	"github.com/weiloon1234/Foundry-Go/auth/token"
	"github.com/weiloon1234/Foundry-Go/clock"
	"github.com/weiloon1234/Foundry-Go/contract/manifest"
	foundryhttp "github.com/weiloon1234/Foundry-Go/http"
	"github.com/weiloon1234/Foundry-Go/openapi"
)

type cookieUser struct{ ID int64 }

func TestOpenAPIDescribesTheRefreshCookieWithoutASecret(t *testing.T) {
	cookie := foundryhttp.DefineRefreshCookie("__Host-refresh-user", foundryhttp.CSRFConfig{})
	route := func(id foundryhttp.RouteID, path string) foundryhttp.Route[foundryhttp.NoPath] {
		return foundryhttp.DefineRoute(foundryhttp.RouteSpec{ID: id, Method: foundryhttp.POST, Access: foundryhttp.Public}, foundryhttp.StaticPath(path))
	}
	refresh := foundryhttp.DefineEndpoint(route("web.refresh", "/refresh"), foundryhttp.EmptyQuery(), foundryhttp.RefreshTokenCookie(cookie), foundryhttp.TokenCookieResponse[cookieUser, int64](cookie, 200, clock.System{}))
	logout := foundryhttp.DefineEndpoint(route("web.logout", "/logout"), foundryhttp.EmptyQuery(), foundryhttp.EmptyBody(), foundryhttp.ClearRefreshCookie(cookie, foundryhttp.EmptyResponse(204)))
	cookieLogout := foundryhttp.DefineEndpoint(route("web.cookie_logout", "/session/logout"), foundryhttp.EmptyQuery(), foundryhttp.RefreshTokenCookieLogout(cookie), foundryhttp.ClearRefreshCookie(cookie, foundryhttp.EmptyResponse(204)))
	router, err := foundryhttp.NewRouter(
		refresh.Handle(func(context.Context, foundryhttp.Input[foundryhttp.NoPath, foundryhttp.NoQuery, foundryhttp.RefreshTokenRequest]) (token.Issued[cookieUser, int64], error) {
			t.Fatal("export invoked handler")
			return token.Issued[cookieUser, int64]{}, nil
		}),
		logout.Handle(func(context.Context, foundryhttp.Input[foundryhttp.NoPath, foundryhttp.NoQuery, foundryhttp.NoBody]) (foundryhttp.NoContent, error) {
			return foundryhttp.NoContent{}, nil
		}),
		cookieLogout.Handle(func(context.Context, foundryhttp.Input[foundryhttp.NoPath, foundryhttp.NoQuery, foundryhttp.RefreshCookieLogoutRequest]) (foundryhttp.NoContent, error) {
			return foundryhttp.NoContent{}, nil
		}),
	)
	if err != nil {
		t.Fatal(err)
	}
	source, err := manifest.Build(t.Context(), manifest.Sources{HTTP: router})
	if err != nil {
		t.Fatal(err)
	}
	snapshot, err := source.Snapshot()
	if err != nil {
		t.Fatal(err)
	}
	for _, operation := range snapshot.HTTP {
		if operation.RefreshCookie == nil || operation.RefreshCookie.Name != cookie.Name() {
			t.Fatal("manifest lost the refresh cookie", operation.Name)
		}
	}
	data, err := openapi.Render(source, openapi.Options{Title: "Portal", APIVersion: "1"})
	if err != nil {
		t.Fatal(err)
	}
	if strings.Contains(string(data), "refresh_token") {
		t.Fatal("OpenAPI exposes a refresh token field")
	}
	var document struct {
		Paths map[string]map[string]struct {
			RequestBody json.RawMessage                `json:"requestBody"`
			Security    []map[string][]string          `json:"security"`
			Responses   map[string]map[string]any      `json:"responses"`
			Cookie      *foundryhttp.RefreshCookieInfo `json:"x-foundry-refresh-cookie"`
		} `json:"paths"`
		Components struct {
			SecuritySchemes map[string]map[string]string `json:"securitySchemes"`
		} `json:"components"`
	}
	if err := json.Unmarshal(data, &document); err != nil {
		t.Fatal(err)
	}
	post := document.Paths["/refresh"]["post"]
	if post.RequestBody != nil || len(post.Security) != 1 || post.Cookie == nil || !post.Cookie.Reads || !post.Cookie.Sets {
		t.Fatal("refresh operation shape", string(data))
	}
	for name := range post.Security[0] {
		scheme := document.Components.SecuritySchemes[name]
		if scheme["type"] != "apiKey" || scheme["in"] != "cookie" || scheme["name"] != "__Host-refresh-user" {
			t.Fatal("refresh cookie security scheme", scheme)
		}
	}
	// A cookie logout reads the cookie when present and also succeeds without it.
	cookieLogoutOperation := document.Paths["/session/logout"]["post"]
	if cookieLogoutOperation.RequestBody != nil || cookieLogoutOperation.Cookie == nil || !cookieLogoutOperation.Cookie.Optional || !cookieLogoutOperation.Cookie.Clears || len(cookieLogoutOperation.Security) != 2 || len(cookieLogoutOperation.Security[0]) != 1 || len(cookieLogoutOperation.Security[1]) != 0 {
		t.Fatal("cookie logout operation shape", cookieLogoutOperation.Security)
	}
	for path, status := range map[string]string{"/refresh": "200", "/logout": "204", "/session/logout": "204"} {
		if _, ok := document.Paths[path]["post"].Responses[status]["headers"]; !ok {
			t.Fatal("success response does not document Set-Cookie", path)
		}
	}
	if _, ok := post.Responses["401"]["headers"]; !ok {
		t.Fatal("401 does not document the clearing Set-Cookie")
	}
}
