package http_test

import (
	"context"
	"errors"
	"net/http"
	"net/http/httptest"
	"strconv"
	"strings"
	"sync/atomic"
	"testing"
	"time"

	"github.com/weiloon1234/Foundry-Go/attribution"
	"github.com/weiloon1234/Foundry-Go/auth"
	foundryhttp "github.com/weiloon1234/Foundry-Go/http"
	"github.com/weiloon1234/Foundry-Go/http/modelbinding"
	"github.com/weiloon1234/Foundry-Go/secret"
	"github.com/weiloon1234/Foundry-Go/testkit"
	"github.com/weiloon1234/Foundry-Go/value"
)

type compositionPath struct{ Order int64 }
type compositionOrder struct{ ID, Owner int64 }
type compositionInput = modelbinding.Input[compositionPath, foundryhttp.NoQuery, foundryhttp.NoBody, compositionOrder]
type compositionCodec struct{ parses *atomic.Int32 }

func (c compositionCodec) Parse(raw string) (int64, error) {
	c.parses.Add(1)
	return strconv.ParseInt(raw, 10, 64)
}
func (compositionCodec) Format(value int64) (string, error) { return strconv.FormatInt(value, 10), nil }
func compositionRoute(access foundryhttp.Access, parses *atomic.Int32) foundryhttp.Route[compositionPath] {
	return foundryhttp.DefineRoute(foundryhttp.RouteSpec{ID: "orders.show", Method: foundryhttp.GET, Access: access}, foundryhttp.DefinePath("/orders/{order}", foundryhttp.Param("order", compositionCodec{parses}, func(p *compositionPath) *int64 { return &p.Order })))
}

const compositionOrigin foundryhttp.Origin = "https://auth.example.test"

func compositionSigner(t *testing.T) (foundryhttp.URLSigner, *testkit.Clock) {
	t.Helper()
	keys, err := foundryhttp.NewSigningKeys(foundryhttp.SigningKey{ID: "fixture", Secret: secret.New(strings.Repeat("k", 32))})
	if err != nil {
		t.Fatal(err)
	}
	now := testkit.NewClock(time.Date(2026, 9, 16, 0, 0, 0, 0, time.UTC))
	signer, err := foundryhttp.NewURLSigner(keys, now)
	if err != nil {
		t.Fatal(err)
	}
	return signer, now
}
func compositionHandler(t *testing.T, registration foundryhttp.RouteRegistration) (http.Handler, *foundryhttp.Router) {
	t.Helper()
	router := newAuthRouter(t, registration)
	handler, err := foundryhttp.ApplyMiddleware(router, foundryhttp.PublicURLs(foundryhttp.PublicURLConfig{AllowedOrigins: []foundryhttp.Origin{compositionOrigin}}))
	if err != nil {
		t.Fatal(err)
	}
	return handler, router
}
func compositionServe(handler http.Handler, location, credential string) *httptest.ResponseRecorder {
	request := httptest.NewRequest("GET", location, nil)
	if credential != "" {
		request.Header.Set("Authorization", "Bearer "+credential)
	}
	response := httptest.NewRecorder()
	handler.ServeHTTP(response, request)
	return response
}
func TestSignedAuthenticatedModelBindingPreservesAllBoundaries(t *testing.T) {
	var loads, parses, lookups atomic.Int32
	_, guard, _ := authSetup(t, "bearer", &loads)
	allowed := true
	permission := auth.DefinePermission("orders.view", func(context.Context, authAccount) (bool, error) { return allowed, nil })
	policy := auth.DefinePolicy("orders.owner", func(_ context.Context, subject authAccount, order compositionOrder) (bool, error) {
		return subject.ID == order.Owner, nil
	})
	registry, err := auth.NewRegistry(auth.DefaultConfig(), guard.Registration(), permission.Registration(), policy.Registration())
	if err != nil {
		t.Fatal(err)
	}
	transport, err := foundryhttp.NewAuthentication(registry, foundryhttp.BearerCredential("bearer"))
	if err != nil {
		t.Fatal(err)
	}
	signer, now := compositionSigner(t)
	endpoint := foundryhttp.DefineEndpoint(compositionRoute(foundryhttp.Guarded, &parses), foundryhttp.EmptyQuery(), foundryhttp.EmptyBody(), foundryhttp.EmptyResponse(204))
	signed := foundryhttp.RequireAuthentication(endpoint, transport, guard).WithPermissions(permission).Signed(signer)
	resolver := modelbinding.Define(func(ctx context.Context, path compositionPath) (value.Optional[compositionOrder], error) {
		lookups.Add(1)
		if origin := attribution.FromContext(ctx); origin.Guard() != "api" {
			t.Error("model lookup lacked verified attribution")
		}
		if path.Order == 404 {
			return value.Optional[compositionOrder]{}, nil
		}
		if path.Order == 500 {
			return value.Set(compositionOrder{ID: path.Order, Owner: 1}), errors.New("private resolver failure")
		}
		return value.Set(compositionOrder{ID: path.Order, Owner: 1}), nil
	})
	bound := modelbinding.BindAuthenticated(signed, resolver)
	var retained []context.Context
	handled := 0
	handler, router := compositionHandler(t, bound.Handle(func(ctx context.Context, subject authAccount, in compositionInput) (foundryhttp.NoContent, error) {
		handled++
		retained = append(retained, ctx)
		if in.Request.Path.Order != in.Model.ID {
			t.Error("resource lost decoded key")
		}
		return foundryhttp.NoContent{}, policy.Authorize(ctx, guard, in.Model)
	}))
	location := func(id int64) string {
		t.Helper()
		raw, err := signed.URL(t.Context(), compositionOrigin, compositionPath{id}, foundryhttp.NoQuery{}, now.Now().Add(time.Minute))
		if err != nil {
			t.Fatal(err)
		}
		return raw
	}
	valid, missing, failed := location(7), location(404), location(500)
	for _, test := range []struct {
		name, url, credential string
		status                int
		resolve, load         bool
	}{
		{"valid", valid, "valid", 204, true, true},
		{"other-owner", valid, "other", 403, true, true},
		{"absent", valid, "", 401, false, false},
		{"invalid", valid, "invalid", 401, false, false},
		{"pending", valid, "pending", 403, false, false},
		{"disabled", valid, "disabled", 401, false, true},
		{"tampered", strings.Replace(valid, "/7?", "/8?", 1), "valid", 403, false, true},
		{"missing", missing, "valid", 404, true, true},
		{"partial-error", failed, "valid", 500, true, true},
		{"unknown-query", valid + "&extra=1", "valid", 403, false, true},
	} {
		t.Run(test.name, func(t *testing.T) {
			beforeParse, beforeLookup, beforeLoad := parses.Load(), lookups.Load(), loads.Load()
			response := compositionServe(handler, test.url, test.credential)
			if response.Code != test.status {
				t.Fatal(response.Code, response.Body.String())
			}
			increment := int32(0)
			if test.resolve {
				increment = 1
			}
			if parses.Load()-beforeParse != increment || lookups.Load()-beforeLookup != increment {
				t.Fatal("decoding/model lookup crossed admission boundary")
			}
			increment = 0
			if test.load {
				increment = 1
			}
			if loads.Load()-beforeLoad != increment {
				t.Fatal("duplicate subject hydration")
			}
			if strings.Contains(response.Body.String(), "private resolver") {
				t.Fatal("resolver cause leaked")
			}
		})
	}
	allowed = false
	before := lookups.Load()
	if response := compositionServe(handler, valid, "valid"); response.Code != 403 || lookups.Load() != before {
		t.Fatal("permission did not guard model binding", response.Code)
	}
	allowed = true
	now.Advance(time.Minute)
	if response := compositionServe(handler, valid, "valid"); response.Code != 403 || lookups.Load() != before {
		t.Fatal("expired signature reached resolver", response.Code)
	}
	if handled != 2 {
		t.Fatal("failed resource reached handler", handled)
	}
	for _, ctx := range retained {
		if _, err := guard.Require(context.WithoutCancel(ctx)); err == nil {
			t.Fatal("composed handler retained auth scope")
		}
	}
	info, err := bound.Description()
	if err != nil {
		t.Fatal(err)
	}
	if info.Route.SignedURL == nil || info.Route.Authentication == nil || len(info.Route.Authentication.RequiredPermissions) != 1 || info.Response != nil || len(info.Query) != 0 {
		t.Fatal("transport metadata lost or models became DTOs")
	}
	if len(router.Endpoints()) != 1 || router.Endpoints()[0].Route.SignedURL == nil {
		t.Fatal("router lost composed endpoint")
	}
}
func TestOptionalSignedModelBindingRetainsOptionalSubject(t *testing.T) {
	var loads, parses atomic.Int32
	registry, guard, _ := authSetup(t, "bearer", &loads)
	transport, err := foundryhttp.NewAuthentication(registry, foundryhttp.BearerCredential("bearer"))
	if err != nil {
		t.Fatal(err)
	}
	signer, now := compositionSigner(t)
	endpoint := foundryhttp.DefineEndpoint(compositionRoute(foundryhttp.Public, &parses), foundryhttp.EmptyQuery(), foundryhttp.EmptyBody(), foundryhttp.EmptyResponse(204))
	signed := foundryhttp.OptionalAuthentication(endpoint, transport, guard).Signed(signer)
	resolver := modelbinding.Define(func(_ context.Context, p compositionPath) (value.Optional[compositionOrder], error) {
		return value.Set(compositionOrder{ID: p.Order}), nil
	})
	bound := modelbinding.BindAuthenticated(signed, resolver)
	var present []bool
	handler, _ := compositionHandler(t, bound.Handle(func(ctx context.Context, subject value.Optional[authAccount], in compositionInput) (foundryhttp.NoContent, error) {
		present = append(present, subject.IsSet())
		_, attributed := attribution.FromContext(ctx).Model()
		if attributed != subject.IsSet() || in.Model.ID != 7 {
			t.Error("optional model/attribution lost")
		}
		return foundryhttp.NoContent{}, nil
	}))
	location, err := signed.URL(t.Context(), compositionOrigin, compositionPath{7}, foundryhttp.NoQuery{}, now.Now().Add(time.Minute))
	if err != nil {
		t.Fatal(err)
	}
	for _, credential := range []string{"", "valid"} {
		if response := compositionServe(handler, location, credential); response.Code != 204 {
			t.Fatal(response.Code, response.Body.String())
		}
	}
	if response := compositionServe(handler, location, "invalid"); response.Code != 401 {
		t.Fatal("invalid optional credential became anonymous")
	}
	if len(present) != 2 || present[0] || !present[1] || loads.Load() != 1 || parses.Load() != 2 {
		t.Fatal("optional composition hydrated incorrectly")
	}
	info, err := bound.Description()
	if err != nil || !info.Route.Authentication.Optional || info.Route.Access != foundryhttp.Public {
		t.Fatal("optional metadata lost", err)
	}
}
