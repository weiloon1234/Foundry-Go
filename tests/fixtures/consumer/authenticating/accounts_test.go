package authenticating_test

import (
	"context"
	"net/http/httptest"
	"sync/atomic"
	"testing"

	"foundry.test/consumer/authenticating"
	"foundry.test/consumer/models"
	"github.com/weiloon1234/Foundry-Go/attribution"
	"github.com/weiloon1234/Foundry-Go/auth"
	foundryhttp "github.com/weiloon1234/Foundry-Go/http"
	"github.com/weiloon1234/Foundry-Go/model"
	"github.com/weiloon1234/Foundry-Go/secret"
	authtest "github.com/weiloon1234/Foundry-Go/testkit/auth"
	"github.com/weiloon1234/Foundry-Go/value"
)

func TestConsumerReceivesGeneratedModelAndReusesProvider(t *testing.T) {
	id, err := model.NewID[models.User]()
	if err != nil {
		t.Fatal(err)
	}
	user := models.User{ID: id, Status: models.StatusActive}
	var loads atomic.Int32
	provider := auth.DefineProvider("users", (models.User{}).FoundryReference(), func(_ context.Context, key model.ID[models.User]) (value.Optional[models.User], error) {
		loads.Add(1)
		if key != id {
			t.Error("wrong typed key")
		}
		return value.Set(user), nil
	}, func(_ context.Context, u models.User) (bool, error) { return u.Status == models.StatusActive, nil })
	proof, err := auth.NewProof(user.FoundryReference(), auth.Authenticated)
	if err != nil {
		t.Fatal(err)
	}
	verifier := func(_ context.Context, token secret.String) (value.Optional[auth.Proof[models.User, model.ID[models.User]]], error) {
		if token.Reveal() != "fixture-token" {
			return value.Optional[auth.Proof[models.User, model.ID[models.User]]]{}, nil
		}
		return value.Set(proof), nil
	}
	registry, api, _, err := authenticating.AccountGuards(provider, auth.DefineStrategy("bearer", verifier), auth.DefineStrategy("session", verifier))
	if err != nil {
		t.Fatal(err)
	}
	transport, err := foundryhttp.NewAuthentication(registry, foundryhttp.BearerCredential("bearer"))
	if err != nil {
		t.Fatal(err)
	}
	router, err := foundryhttp.NewRouter(authenticating.ProfileRoute(authenticating.PermissionProfile(authenticating.Profile(transport, api)), api))
	if err != nil {
		t.Fatal(err)
	}
	for range 2 {
		request := httptest.NewRequest("GET", "/profile", nil)
		request.Header.Set("Authorization", "Bearer fixture-token")
		recorder := httptest.NewRecorder()
		router.ServeHTTP(recorder, request)
		if recorder.Code != 204 {
			t.Fatal(recorder.Code, recorder.Body.String())
		}
	}
	if loads.Load() != 2 {
		t.Fatal("model loaded more than once per request", loads.Load())
	}
	scope := authtest.Scope(t, registry, auth.Credential{Name: "bearer", Secret: secret.New("fixture-token")})
	authenticated := authtest.Require(t, scope, api)
	if authenticated.ID != user.ID {
		t.Fatal("test helper lost model type/identity")
	}
	attributed, err := authenticating.AttributeOperation(scope.Context(), api)
	if err != nil {
		t.Fatal(err)
	}
	if err := authenticating.CheckAccountPermission(attributed, api); err != nil {
		t.Fatal(err)
	}
	origin, err := authenticating.CaptureOrigin(attributed, api)
	if err != nil || origin != attribution.FromContext(attributed) || origin.Guard() != "users.api" {
		t.Fatal("consumer attribution lost guard", err)
	}
	if loads.Load() != 3 {
		t.Fatal("consumer attribution repeated hydration")
	}
	identity, err := user.FoundryIdentity()
	if err != nil {
		t.Fatal(err)
	}
	restored, err := authenticating.RestoreIdentity(provider, identity)
	if err != nil || restored.Key() != user.ID {
		t.Fatal("stored model key lost", err)
	}
}
