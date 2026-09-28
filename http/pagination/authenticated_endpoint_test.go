package pagination_test

import (
	"context"
	"errors"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/weiloon1234/Foundry-Go/auth"
	"github.com/weiloon1234/Foundry-Go/database/codec"
	"github.com/weiloon1234/Foundry-Go/database/query"
	foundryhttp "github.com/weiloon1234/Foundry-Go/http"
	"github.com/weiloon1234/Foundry-Go/http/pagination"
	"github.com/weiloon1234/Foundry-Go/model"
	"github.com/weiloon1234/Foundry-Go/secret"
	"github.com/weiloon1234/Foundry-Go/value"
)

type pageActor struct {
	ID      int64
	Private string
}

func (a pageActor) reference() model.Reference[pageActor, int64] {
	return model.NewReference[pageActor]("page_actors", a.ID, codec.Signed[int64]())
}
func (a pageActor) FoundryIdentity() (model.Identity, error) { return a.reference().Identity() }

func pageBinding(t *testing.T) (foundryhttp.GuardBinding[pageActor], auth.AccessScopes[pageActor], auth.Permission[pageActor]) {
	t.Helper()
	scopes, err := auth.NewAccessScopes(auth.DefineAccessScope[pageActor]("pages.read"))
	if err != nil {
		t.Fatal(err)
	}
	provider := auth.DefineProvider("page_actors", (pageActor{}).reference(), func(_ context.Context, id int64) (value.Optional[pageActor], error) {
		return value.Set(pageActor{ID: id, Private: "private-actor-field"}), nil
	}, func(context.Context, pageActor) (bool, error) { return true, nil })
	strategy := auth.DefineStrategy("bearer", func(_ context.Context, token secret.String) (value.Optional[auth.Proof[pageActor, int64]], error) {
		id, ceiling := int64(1), scopes
		switch token.Reveal() {
		case "valid":
		case "scope":
			ceiling = auth.AccessScopes[pageActor]{}
		case "permission":
			id = 2
		case "policy":
			id = 3
		default:
			return value.Optional[auth.Proof[pageActor, int64]]{}, nil
		}
		proof, err := auth.NewScopedProof((pageActor{ID: id}).reference(), auth.Authenticated, ceiling)
		return value.Set(proof), err
	})
	guard := auth.DefineGuard("pages", provider, strategy)
	permission := auth.DefinePermission("pages.view", func(_ context.Context, actor pageActor) (bool, error) { return actor.ID != 2, nil })
	registry, err := auth.NewRegistry(auth.DefaultConfig(), guard.Registration(), permission.Registration())
	if err != nil {
		t.Fatal(err)
	}
	authentication, err := foundryhttp.NewAuthentication(registry, foundryhttp.BearerCredential("bearer"))
	if err != nil {
		t.Fatal(err)
	}
	binding, err := foundryhttp.BindGuard(authentication, guard)
	if err != nil {
		t.Fatal(err)
	}
	return binding, scopes, permission
}

func guardedPageRoute() foundryhttp.Route[foundryhttp.NoPath] {
	return foundryhttp.DefineRoute(foundryhttp.RouteSpec{ID: "items.secure", Method: foundryhttp.GET, Access: foundryhttp.Guarded}, foundryhttp.StaticPath("/items"))
}
func pageResponse(t *testing.T, registration foundryhttp.RouteRegistration, target, token string) *httptest.ResponseRecorder {
	t.Helper()
	router, err := foundryhttp.NewRouter(registration)
	if err != nil {
		t.Fatal(err)
	}
	request := httptest.NewRequest("GET", target, nil)
	if token != "" {
		request.Header.Set("Authorization", "Bearer "+token)
	}
	response := httptest.NewRecorder()
	router.ServeHTTP(response, request)
	return response
}

func TestAuthenticatedPagesPreserveAuthorityBeforePageReads(t *testing.T) {
	binding, scopes, permission := pageBinding(t)
	base := pagination.DefineNumbered(guardedPageRoute(), filterQuery(), itemJSON(), pagination.DefaultConfig()).Within(foundryhttp.DefineScope("/api", "api"))
	policies, reads := 0, 0
	endpoint := pagination.Authenticated(base, binding).WithScopes(scopes).WithPermissions(permission).WithAuthorization(func(ctx context.Context, actor pageActor, in pageInput) error {
		policies++
		if actor.ID == 3 {
			return auth.Forbidden
		}
		if in.Page.Number != 1 || in.Page.Size != 20 {
			t.Error("unvalidated request reached policy")
		}
		return nil
	})
	registration := endpoint.Handle(func(ctx context.Context, actor pageActor, in pageInput) (query.Page[Item], error) {
		reads++
		again, err := binding.Guard().Require(ctx)
		if err != nil || actor != again || actor.ID != 1 {
			t.Error("actor not delivered/reused", err)
		}
		return query.Page[Item]{Items: []Item{{Label: "visible"}}, Number: in.Page.Number, Size: in.Page.Size, Total: 21, Pages: 2}, nil
	})
	for _, tc := range []struct {
		token, suffix string
		status        int
	}{
		{"", "?page=bad", 401}, {"invalid", "", 401},
		{"scope", "?page=bad", 403}, {"permission", "?page=bad", 403}, {"policy", "", 403},
		{"valid", "?page=bad", 400}, {"valid", "?per_page=101", 422}, {"valid", "?page=0", 422},
		{"valid", "?page=1&page=2", 400}, {"valid", "?unknown=secret", 400}, {"valid", "?q=a%2Bb", 200},
	} {
		response := pageResponse(t, registration, "/api/items"+tc.suffix, tc.token)
		if response.Code != tc.status {
			t.Fatalf("%s %s: %d %s", tc.token, tc.suffix, response.Code, response.Body)
		}
		if strings.Contains(response.Body.String(), "private-actor-field") {
			t.Fatal("actor leaked into DTO")
		}
		if tc.status == 200 {
			result := decodeNumbered(t, response)
			next, _ := result.Links.Next.Get()
			if next != "/api/items?page=2&per_page=20&q=a%2Bb" {
				t.Fatal("scoped filter link lost", next)
			}
		}
	}
	if reads != 1 || policies != 2 {
		t.Fatal("denied/invalid request reached page read", reads, policies)
	}
	location, err := endpoint.URL(t.Context(), foundryhttp.NoPath{}, filters{}, query.PageRequest{Number: 2, Size: 20})
	if err != nil || location != "/api/items?page=2&per_page=20" {
		t.Fatal(location, err)
	}
	info, err := endpoint.Description()
	if err != nil || info.Route.Authentication == nil {
		t.Fatal("missing authentication metadata", err)
	}
	if info.Route.Authentication.Guard != "pages" || len(info.Route.Authentication.RequiredScopes) != 1 || len(info.Route.Authentication.RequiredPermissions) != 1 {
		t.Fatal("authority metadata lost")
	}
	info.Route.Authentication.RequiredScopes[0] = "mutated"
	again, err := endpoint.Description()
	if err != nil || again.Route.Authentication.RequiredScopes[0] != "pages.read" {
		t.Fatal("metadata aliases caller", err)
	}
}

func TestAuthenticatedSimpleAndCursorKeepNavigationAndValidation(t *testing.T) {
	binding, _, _ := pageBinding(t)
	simple := pagination.Authenticated(pagination.DefineSimple(guardedPageRoute(), filterQuery(), itemJSON(), pagination.DefaultConfig()), binding)
	simpleRegistration := simple.Handle(func(_ context.Context, actor pageActor, in pageInput) (query.SimplePage[Item], error) {
		if actor.ID != 1 {
			t.Error("simple actor lost")
		}
		return query.SimplePage[Item]{Items: []Item{{Label: "visible"}}, Number: in.Page.Number, Size: in.Page.Size, HasMore: true}, nil
	})
	if pageResponse(t, simpleRegistration, "/items?per_page=1", "").Code != 401 {
		t.Fatal("simple accepted anonymous")
	}
	response := pageResponse(t, simpleRegistration, "/items?per_page=1", "valid")
	result, err := pagination.SimpleJSON(itemJSON()).Decode(t.Context(), response.Body.Bytes(), foundryhttp.DefaultEndpointLimits().Response)
	if response.Code != 200 || err != nil || result.Links.Next.IsNull() || strings.Contains(response.Body.String(), "total") {
		t.Fatal("simple page changed", response.Body, err)
	}

	token := tokenFor[cursorSource](t)
	cursor := pagination.Authenticated(pagination.DefineCursor[cursorSource](guardedPageRoute(), filterQuery(), itemJSON(), pagination.DefaultCursorConfig()), binding)
	reads := 0
	cursorRegistration := cursor.Handle(func(ctx context.Context, actor pageActor, in cursorInput) (pagination.CursorResult[cursorSource, Item], error) {
		reads++
		if actor.ID != 1 {
			t.Error("cursor actor lost")
		}
		if term, _ := in.Filters.Term.Get(); term == "mismatch" {
			return pagination.CursorResult[cursorSource, Item]{}, nil
		}
		if term, _ := in.Filters.Term.Get(); term == "scope" {
			return pagination.CursorResult[cursorSource, Item]{}, (query.CursorRequest[cursorSource]{}).Validate()
		}
		return pagination.MapCursorPage(ctx, query.CursorPage[cursorSource]{Items: []cursorSource{{Stored: "visible"}}, Size: in.Page.Size, Next: value.Set(token)}, cursorDTO)
	})
	for _, tc := range []struct {
		suffix, credential string
		status             int
	}{
		{"?after=bad", "", 401}, {"?after=bad", "valid", 400}, {"?per_page=101", "valid", 422},
		{"?after=" + token.Token() + "&before=" + token.Token(), "valid", 422},
		{"?q=scope", "valid", 400}, {"?q=mismatch", "valid", 500}, {"?q=visible", "valid", 200},
	} {
		response = pageResponse(t, cursorRegistration, "/items"+tc.suffix, tc.credential)
		if response.Code != tc.status {
			t.Fatalf("cursor %s: %d %s", tc.suffix, response.Code, response.Body)
		}
	}
	if reads != 3 {
		t.Fatal("invalid cursor reached read", reads)
	}
	got, err := pagination.CursorJSON(itemJSON()).Decode(t.Context(), response.Body.Bytes(), foundryhttp.DefaultEndpointLimits().Response)
	if err != nil || got.Links.Next.IsNull() || got.Data[0].Label != "display:visible" {
		t.Fatal("cursor DTO/navigation lost", err)
	}
}

func TestAuthenticatedPageCompletionKeepsErrorsCancellationAndInvalidDeclarations(t *testing.T) {
	binding, _, _ := pageBinding(t)
	declared := foundryhttp.DefineError("pages.unavailable", 409, "Page changed.")
	base := pagination.DefineNumbered(guardedPageRoute(), filterQuery(), itemJSON(), pagination.DefaultConfig()).WithErrors(declared)
	endpoint := pagination.Authenticated(base, binding)
	for _, tc := range []struct {
		page   query.Page[Item]
		err    error
		status int
	}{
		{query.Page[Item]{}, nil, 500},
		{query.Page[Item]{Number: 1, Size: 20}, errors.New("private-handler-error"), 500},
		{query.Page[Item]{}, declared.WithCause(auth.Forbidden), 409},
		{query.Page[Item]{Number: 1, Size: 20}, nil, 200},
	} {
		response := pageResponse(t, endpoint.Handle(func(context.Context, pageActor, pageInput) (query.Page[Item], error) { return tc.page, tc.err }), "/items", "valid")
		if response.Code != tc.status || strings.Contains(response.Body.String(), "private-handler-error") {
			t.Fatal("completion/error mapping changed", response.Code, response.Body)
		}
	}
	ctx, cancel := context.WithCancel(t.Context())
	router, err := foundryhttp.NewRouter(endpoint.Handle(func(context.Context, pageActor, pageInput) (query.Page[Item], error) {
		cancel()
		return query.Page[Item]{Number: 1, Size: 20}, nil
	}))
	if err != nil {
		t.Fatal(err)
	}
	req := httptest.NewRequest("GET", "/items", nil).WithContext(ctx)
	req.Header.Set("Authorization", "Bearer valid")
	response := httptest.NewRecorder()
	router.ServeHTTP(response, req)
	if strings.Contains(response.Body.String(), `"data"`) {
		t.Fatal("cancellation published a page")
	}
	for _, registration := range []foundryhttp.RouteRegistration{
		endpoint.Handle(nil),
		endpoint.WithAuthorization(nil).Handle(func(context.Context, pageActor, pageInput) (query.Page[Item], error) { return query.Page[Item]{}, nil }),
		pagination.Authenticated(pagination.DefineNumbered(guardedPageRoute(), filterQuery(), itemJSON(), pagination.Config{}), binding).Handle(func(context.Context, pageActor, pageInput) (query.Page[Item], error) { return query.Page[Item]{}, nil }),
		pagination.Authenticated(base, foundryhttp.GuardBinding[pageActor]{}).Handle(func(context.Context, pageActor, pageInput) (query.Page[Item], error) { return query.Page[Item]{}, nil }),
	} {
		if _, err := foundryhttp.NewRouter(registration); err == nil {
			t.Fatal("invalid authenticated page assembled")
		}
	}
	if endpoint.WithAuthorization(nil).Validate() == nil || endpoint.Validate() != nil {
		t.Fatal("authorization mutation changed original")
	}
}
