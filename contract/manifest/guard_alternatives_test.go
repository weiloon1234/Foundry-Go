package manifest

import (
	"bytes"
	"encoding/json"
	"fmt"
	"reflect"
	"testing"

	"github.com/weiloon1234/Foundry-Go/auth"
	h "github.com/weiloon1234/Foundry-Go/http"
)

func alternativeDocument(t *testing.T) Document {
	t.Helper()
	base := errorDetailsManifest(t, errorDetailsRoute(t, "editorial.read", h.GET))
	doc, err := base.Snapshot()
	if err != nil {
		t.Fatal(err)
	}
	route := &doc.HTTP[0].Route
	route.Access = h.Guarded
	route.Authentication = &h.AuthenticationInfo{
		Guard: "editorial", Provider: "actors",
		Credential: h.CredentialInfo{Source: "bearer", Kind: h.BearerCredentialKind, Name: "Authorization"},
		AnyOf: []h.GuardAlternativeInfo{
			{RequiredScopes: []auth.AccessScopeName{"editorial.write"}, RequiredPermissions: []auth.PermissionName{"editorial.write"}},
			{RequiredScopes: []auth.AccessScopeName{"editorial.approve"}, RequiredPermissions: []auth.PermissionName{"editorial.approve"}},
		},
	}
	raw := *route
	raw.ID, raw.Path, raw.Raw = "editorial.raw", "/editorial/raw", true
	doc.RawRoutes = []h.RouteInfo{raw}
	return doc
}

func TestManifestGuardAlternativesRoundtripProjectionAndOwnership(t *testing.T) {
	doc := alternativeDocument(t)
	source, err := freeze(doc)
	if err != nil {
		t.Fatal(err)
	}
	data, _ := source.JSON()
	restored, err := Decode(data)
	if err != nil {
		t.Fatal(err)
	}
	again, _ := restored.JSON()
	if !bytes.Equal(data, again) {
		t.Fatal("alternative order changed on roundtrip")
	}
	selected, err := restored.Project(Selection{Routes: []h.RouteID{"editorial"}})
	if err != nil {
		t.Fatal(err)
	}
	snapshot, _ := selected.Snapshot()
	if len(snapshot.HTTP) != 1 || len(snapshot.RawRoutes) != 1 || !reflect.DeepEqual(snapshot.HTTP[0].Route.Authentication, doc.HTTP[0].Route.Authentication) || !reflect.DeepEqual(snapshot.RawRoutes[0].Authentication, doc.RawRoutes[0].Authentication) {
		t.Fatal("projection flattened or dropped alternatives")
	}
	snapshot.HTTP[0].Route.Authentication.AnyOf[0].RequiredScopes[0] = "mutated"
	snapshot.RawRoutes[0].Authentication.AnyOf[1].RequiredPermissions[0] = "mutated"
	owned, _ := selected.Snapshot()
	if !reflect.DeepEqual(owned.HTTP[0].Route.Authentication, doc.HTTP[0].Route.Authentication) || !reflect.DeepEqual(owned.RawRoutes[0].Authentication, doc.RawRoutes[0].Authentication) {
		t.Fatal("snapshot aliases nested guard requirements")
	}
	doc.Version = 9
	old, _ := json.Marshal(doc)
	if _, err := Decode(old); err == nil {
		t.Fatal("v9 alternative metadata accepted")
	}
}

func TestManifestRejectsMalformedGuardAlternatives(t *testing.T) {
	cases := map[string]func(*h.AuthenticationInfo){
		"empty":                func(a *h.AuthenticationInfo) { a.AnyOf = []h.GuardAlternativeInfo{} },
		"empty branch":         func(a *h.AuthenticationInfo) { a.AnyOf[1] = h.GuardAlternativeInfo{} },
		"empty scopes":         func(a *h.AuthenticationInfo) { a.AnyOf[1].RequiredScopes = []auth.AccessScopeName{} },
		"empty permissions":    func(a *h.AuthenticationInfo) { a.AnyOf[1].RequiredPermissions = []auth.PermissionName{} },
		"duplicate":            func(a *h.AuthenticationInfo) { a.AnyOf[1] = a.AnyOf[0] },
		"duplicate scope":      func(a *h.AuthenticationInfo) { a.AnyOf[1].RequiredScopes = []auth.AccessScopeName{"a", "a"} },
		"duplicate permission": func(a *h.AuthenticationInfo) { a.AnyOf[1].RequiredPermissions = []auth.PermissionName{"a", "a"} },
		"invalid scope":        func(a *h.AuthenticationInfo) { a.AnyOf[1].RequiredScopes = []auth.AccessScopeName{"*"} },
		"invalid permission":   func(a *h.AuthenticationInfo) { a.AnyOf[1].RequiredPermissions = []auth.PermissionName{"*"} },
		"mixed scopes":         func(a *h.AuthenticationInfo) { a.RequiredScopes = []auth.AccessScopeName{"a"} },
		"mixed permissions":    func(a *h.AuthenticationInfo) { a.RequiredPermissions = []auth.PermissionName{"a"} },
		"optional":             func(a *h.AuthenticationInfo) { a.Optional = true },
		"branch bound":         func(a *h.AuthenticationInfo) { a.AnyOf = make([]h.GuardAlternativeInfo, h.MaxGuardAlternatives+1) },
		"scope total": func(a *h.AuthenticationInfo) {
			for i := range a.AnyOf {
				a.AnyOf[i].RequiredScopes = nil
				for j := range auth.MaxAccessScopes/2 + 1 {
					a.AnyOf[i].RequiredScopes = append(a.AnyOf[i].RequiredScopes, auth.AccessScopeName(fmt.Sprintf("scope.%d", j)))
				}
			}
		},
		"permission total": func(a *h.AuthenticationInfo) {
			for i := range a.AnyOf {
				a.AnyOf[i].RequiredPermissions = nil
				for j := range auth.MaxRegistrations/2 + 1 {
					a.AnyOf[i].RequiredPermissions = append(a.AnyOf[i].RequiredPermissions, auth.PermissionName(fmt.Sprintf("permission.%d", j)))
				}
			}
		},
	}
	for name, change := range cases {
		for _, raw := range []bool{false, true} {
			t.Run(fmt.Sprintf("%s/raw-%t", name, raw), func(t *testing.T) {
				doc := alternativeDocument(t)
				// Keep the other route independent so the selected path owns failure.
				copy := *doc.RawRoutes[0].Authentication
				copy.AnyOf = append([]h.GuardAlternativeInfo(nil), copy.AnyOf...)
				doc.RawRoutes[0].Authentication = &copy
				selected := doc.HTTP[0].Route.Authentication
				if raw {
					selected = doc.RawRoutes[0].Authentication
				}
				change(selected)
				if _, err := freeze(doc); err == nil {
					t.Fatal("invalid metadata accepted")
				}
			})
		}
	}
	doc := alternativeDocument(t)
	doc.HTTP[0].Route.Access = h.Public
	if _, err := freeze(doc); err == nil {
		t.Fatal("public alternative guard accepted")
	}
}
