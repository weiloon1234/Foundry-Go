package openapi_test

import (
	"context"
	"encoding/json"
	"reflect"
	"strings"
	"testing"

	"github.com/weiloon1234/Foundry-Go/auth"
	"github.com/weiloon1234/Foundry-Go/contract/manifest"
	h "github.com/weiloon1234/Foundry-Go/http"
	"github.com/weiloon1234/Foundry-Go/openapi"
	"github.com/weiloon1234/Foundry-Go/typescript"
)

func TestOpenAPIAnyOfKeepsCredentialAndRefreshCookieConjunction(t *testing.T) {
	for _, credential := range []h.CredentialInfo{
		{Source: "api", Kind: h.BearerCredentialKind, Name: "Authorization"},
		{Source: "api", Kind: h.CookieCredentialKind, Name: "session", OriginProtection: true},
		{Source: "api", Kind: h.TicketCredentialKind, Name: "ticket"},
	} {
		t.Run(string(credential.Kind), func(t *testing.T) {
			endpoint := h.DefineEndpoint(h.DefineRoute(h.RouteSpec{ID: "editorial.read", Method: h.POST, Access: h.Public}, h.StaticPath("/editorial")), h.EmptyQuery(), h.EmptyBody(), h.EmptyResponse(204))
			router, err := h.NewRouter(endpoint.Handle(func(context.Context, h.Input[h.NoPath, h.NoQuery, h.NoBody]) (h.NoContent, error) {
				return h.NoContent{}, nil
			}))
			if err != nil {
				t.Fatal(err)
			}
			source, err := manifest.Build(t.Context(), manifest.Sources{HTTP: router})
			if err != nil {
				t.Fatal(err)
			}
			doc, _ := source.Snapshot()
			a := &h.AuthenticationInfo{Guard: "editorial", Provider: "actors", Credential: credential, AnyOf: []h.GuardAlternativeInfo{
				{RequiredScopes: []auth.AccessScopeName{"editorial.write"}, RequiredPermissions: []auth.PermissionName{"editorial.write"}},
				{RequiredScopes: []auth.AccessScopeName{"editorial.approve"}, RequiredPermissions: []auth.PermissionName{"editorial.approve"}},
			}}
			doc.HTTP[0].Route.Access, doc.HTTP[0].Route.Authentication = h.Guarded, a
			// A second operation shares the scheme but uses the legacy flat form.
			legacy := doc.HTTP[0]
			legacy.Name, legacy.Route.ID, legacy.Route.Path = "legacyRead", "legacy.read", "/legacy"
			legacyAuth := *a
			legacyAuth.AnyOf, legacyAuth.RequiredPermissions = nil, []auth.PermissionName{"editorial.write"}
			legacy.Route.Authentication = &legacyAuth
			doc.HTTP = append(doc.HTTP, legacy)
			// Refresh-cookie metadata is independent of request payload schemas.
			doc.HTTP[0].RefreshCookie = &h.RefreshCookieInfo{Name: "__Host-refresh", Reads: true, Clears: true}
			encoded, _ := json.Marshal(doc)
			source, err = manifest.Decode(encoded)
			if err != nil {
				t.Fatal(err)
			}
			data, err := openapi.Render(source, openapi.Options{Title: "Editorial", APIVersion: "1"})
			if err != nil {
				t.Fatal(err)
			}
			var exported struct {
				Paths map[string]map[string]struct {
					Security       []map[string][]string `json:"security"`
					Authentication h.AuthenticationInfo  `json:"x-foundry-authentication"`
				} `json:"paths"`
				Components struct {
					Schemes map[string]map[string]string `json:"securitySchemes"`
				} `json:"components"`
			}
			if err := json.Unmarshal(data, &exported); err != nil {
				t.Fatal(err)
			}
			op := exported.Paths["/editorial"]["post"]
			if len(op.Security) != 2 || !reflect.DeepEqual(op.Authentication, *a) {
				t.Fatal("grouped declaration changed")
			}
			for i, branch := range op.Security {
				if len(branch) != 2 {
					t.Fatal("refresh cookie missing from an alternative", branch)
				}
				var roles []string
				for name, required := range branch {
					scheme := exported.Components.Schemes[name]
					if scheme["name"] == "__Host-refresh" {
						if len(required) != 0 || scheme["in"] != "cookie" {
							t.Fatal("refresh credential changed")
						}
					} else {
						roles = required
						if scheme["type"] == "oauth2" || !strings.Contains(scheme["description"], "scope:<name>") {
							t.Fatal("real scheme or role explanation lost")
						}
					}
				}
				want := []string{"scope:" + string(a.AnyOf[i].RequiredScopes[0]), "permission:" + string(a.AnyOf[i].RequiredPermissions[0])}
				if !reflect.DeepEqual(roles, want) {
					t.Fatal("alternative pairing lost", roles)
				}
			}
			flat := exported.Paths["/legacy"]["post"].Security
			if len(flat) != 1 || len(flat[0]) != 1 {
				t.Fatal("legacy credential changed")
			}
			for _, roles := range flat[0] {
				if len(roles) != 0 {
					t.Fatal("legacy security roles changed")
				}
			}
			client, err := typescript.Render(source)
			if err != nil {
				t.Fatal(err)
			}
			if !strings.Contains(string(client), "readonly any_of:") || !strings.Contains(string(client), "readonly any_of?: never") || !strings.Contains(string(client), "editorial.approve") {
				t.Fatal("TypeScript lost readonly alternative metadata")
			}
		})
	}
}
