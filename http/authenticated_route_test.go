package http_test

import (
	"context"
	"net/http"
	"net/http/httptest"
	"strings"
	"sync/atomic"
	"testing"
	"time"

	"github.com/weiloon1234/Foundry-Go/attribution"
	"github.com/weiloon1234/Foundry-Go/auth"
	foundryhttp "github.com/weiloon1234/Foundry-Go/http"
	"github.com/weiloon1234/Foundry-Go/value"
)

func TestAuthenticatedRawRoutesPreserveNativeTransportAndRequirements(t *testing.T) {
	for _, signedMode := range []bool{false, true} {
		name := "ordinary"
		if signedMode {
			name = "signed"
		}
		t.Run(name, func(t *testing.T) {
			var loads, parses atomic.Int32
			_, guard, _ := authSetup(t, "bearer", &loads)
			permitted := true
			permission := auth.DefinePermission("raw.write", func(context.Context, authAccount) (bool, error) { return permitted, nil })
			registry, err := auth.NewRegistry(auth.DefaultConfig(), guard.Registration(), permission.Registration())
			if err != nil {
				t.Fatal(err)
			}
			transport, err := foundryhttp.NewAuthentication(registry, foundryhttp.BearerCredential("bearer"))
			if err != nil {
				t.Fatal(err)
			}
			route := foundryhttp.DefineRoute(foundryhttp.RouteSpec{ID: "raw.write", Method: foundryhttp.POST, Access: foundryhttp.Guarded}, foundryhttp.DefinePath("/raw/{order}", foundryhttp.Param("order", compositionCodec{&parses}, func(p *compositionPath) *int64 { return &p.Order })))
			bound := foundryhttp.RequireRouteAuthentication(route, transport, guard).WithPermissions(permission)
			signer, now := compositionSigner(t)
			var original *http.Request
			var writer *httptest.ResponseRecorder
			handled := 0
			var retained context.Context
			raw := func(w http.ResponseWriter, request *http.Request, subject authAccount, path compositionPath) {
				handled++
				retained = request.Context()
				if w != writer || request.URL != original.URL || request.RequestURI != original.RequestURI || request.Body != original.Body || request.Header.Get("X-Native") != "kept" {
					t.Error("native request/writer changed")
				}
				if _, ok := w.(http.Flusher); !ok {
					t.Error("writer capability lost")
				}
				if subject.ID != 1 || path.Order != 7 || attribution.FromContext(request.Context()).Guard() != "api" {
					t.Error("typed subject/path/attribution lost")
				}
				w.WriteHeader(204)
			}
			registration := bound.HandleRaw(raw)
			location, err := bound.URL(compositionPath{7})
			if err != nil {
				t.Fatal(err)
			}
			info, err := bound.Description()
			if err != nil {
				t.Fatal(err)
			}
			if signedMode {
				signed := bound.Signed(signer)
				registration = signed.HandleRaw(raw)
				location, err = signed.URL(t.Context(), compositionOrigin, compositionPath{7}, now.Now().Add(time.Minute))
				if err != nil {
					t.Fatal(err)
				}
				info, err = signed.Description()
				if err != nil {
					t.Fatal(err)
				}
				if info.SignedURL == nil {
					t.Fatal("lost signature metadata")
				}
			}
			handler, router := compositionHandler(t, registration)
			if !info.Raw || info.Authentication == nil || info.Authentication.RequiredPermissions[0] != "raw.write" || len(router.Endpoints()) != 0 {
				t.Fatal("raw route lost auth or claimed DTO contract")
			}
			serve := func(url, credential string) *httptest.ResponseRecorder {
				original = httptest.NewRequest("POST", url, strings.NewReader("native request payload"))
				original.Header.Set("X-Native", "kept")
				if credential != "" {
					original.Header.Set("Authorization", "Bearer "+credential)
				}
				writer = httptest.NewRecorder()
				handler.ServeHTTP(writer, original)
				return writer
			}
			if !signedMode {
				location = string(compositionOrigin) + location
			}
			if response := serve(location, "valid"); response.Code != 204 {
				t.Fatal(response.Code, response.Body.String())
			}
			if handled != 1 || parses.Load() != 1 || loads.Load() != 1 {
				t.Fatal("native auth duplicated hydration")
			}
			if _, err := guard.Require(context.WithoutCancel(retained)); err == nil {
				t.Fatal("native handler escaped scope lifetime")
			}
			for _, credential := range []string{"", "invalid", "pending", "disabled"} {
				before := parses.Load()
				response := serve(location, credential)
				if response.Code == 204 || parses.Load() != before || handled != 1 {
					t.Fatal("native auth failed open", response.Code)
				}
			}
			permitted = false
			before := parses.Load()
			if response := serve(location, "valid"); response.Code != 403 || parses.Load() != before {
				t.Fatal("raw permission bypassed")
			}
			permitted = true
			if signedMode {
				if response := serve(strings.Replace(location, "/7?", "/8?", 1), "valid"); response.Code != 403 || parses.Load() != before {
					t.Fatal("raw signature decoded before validation")
				}
				now.Advance(time.Minute)
				if response := serve(location, "valid"); response.Code != 403 || handled != 1 {
					t.Fatal("expired raw link accepted")
				}
			}
			if _, err := foundryhttp.NewRouter(bound.HandleRaw(nil)); err == nil {
				t.Fatal("nil raw handler accepted")
			}
			info.Authentication.RequiredPermissions[0] = "changed"
			if router.Routes()[0].Authentication.RequiredPermissions[0] != "raw.write" {
				t.Fatal("raw inspection mutated requirements")
			}
		})
	}
}
func TestOptionalRawRoutesKeepAnonymousDistinctFromInvalid(t *testing.T) {
	var loads, parses atomic.Int32
	registry, guard, _ := authSetup(t, "bearer", &loads)
	transport, err := foundryhttp.NewAuthentication(registry, foundryhttp.BearerCredential("bearer"))
	if err != nil {
		t.Fatal(err)
	}
	route := compositionRoute(foundryhttp.Public, &parses)
	optional := foundryhttp.OptionalRouteAuthentication(route, transport, guard)
	signer, now := compositionSigner(t)
	signed := optional.Signed(signer)
	location, err := signed.URL(t.Context(), compositionOrigin, compositionPath{7}, now.Now().Add(time.Minute))
	if err != nil {
		t.Fatal(err)
	}
	var present []bool
	handler, router := compositionHandler(t, signed.HandleRaw(func(w http.ResponseWriter, r *http.Request, subject value.Optional[authAccount], path compositionPath) {
		present = append(present, subject.IsSet())
		_, attributed := attribution.FromContext(r.Context()).Model()
		if attributed != subject.IsSet() || path.Order != 7 {
			t.Error("lost optional raw state")
		}
		w.WriteHeader(204)
	}))
	for _, credential := range []string{"", "valid"} {
		if response := compositionServe(handler, location, credential); response.Code != 204 {
			t.Fatal(response.Code, response.Body.String())
		}
	}
	if response := compositionServe(handler, location, "invalid"); response.Code != 401 {
		t.Fatal("invalid raw credential became anonymous")
	}
	if len(present) != 2 || present[0] || !present[1] || loads.Load() != 1 || parses.Load() != 2 {
		t.Fatal("optional native hydration changed")
	}
	info := router.Routes()[0]
	if !info.Raw || info.SignedURL == nil || !info.Authentication.Optional {
		t.Fatal("lost optional signed raw metadata")
	}
	if _, err := foundryhttp.NewRouter(optional.HandleRaw(nil)); err == nil {
		t.Fatal("nil optional raw handler accepted")
	}
}
func TestRawAuthenticationRejectsWrongAccessAndUnregisteredGuard(t *testing.T) {
	var loads, parses atomic.Int32
	registry, guard, _ := authSetup(t, "bearer", &loads)
	transport, err := foundryhttp.NewAuthentication(registry, foundryhttp.BearerCredential("bearer"))
	if err != nil {
		t.Fatal(err)
	}
	if err := foundryhttp.RequireRouteAuthentication(compositionRoute(foundryhttp.Public, &parses), transport, guard).Validate(); err == nil {
		t.Fatal("required binding accepted public declaration")
	}
	if err := foundryhttp.OptionalRouteAuthentication(compositionRoute(foundryhttp.Guarded, &parses), transport, guard).Validate(); err == nil {
		t.Fatal("optional binding weakened guarded declaration")
	}
	empty, err := auth.NewRegistry(auth.DefaultConfig())
	if err != nil {
		t.Fatal(err)
	}
	other, err := foundryhttp.NewAuthentication(empty, foundryhttp.BearerCredential("bearer"))
	if err != nil {
		t.Fatal(err)
	}
	if err := foundryhttp.RequireRouteAuthentication(compositionRoute(foundryhttp.Guarded, &parses), other, guard).Validate(); err == nil {
		t.Fatal("unregistered raw guard accepted")
	}
	if loads.Load() != 0 {
		t.Fatal("route validation performed model lookup")
	}
}
