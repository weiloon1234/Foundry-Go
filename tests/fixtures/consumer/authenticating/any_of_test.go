package authenticating_test

import (
	"context"
	"net/http/httptest"
	"testing"
	"time"

	"foundry.test/consumer/authenticating"
	"foundry.test/consumer/models"
	"github.com/weiloon1234/Foundry-Go/auth"
	"github.com/weiloon1234/Foundry-Go/database/orm"
	h "github.com/weiloon1234/Foundry-Go/http"
	"github.com/weiloon1234/Foundry-Go/model"
	"github.com/weiloon1234/Foundry-Go/secret"
	"github.com/weiloon1234/Foundry-Go/value"
)

func TestEditorialAlternativesKeepActorScopeAndResourceTypes(t *testing.T) {
	id, err := model.NewIDAt[models.User](time.Now())
	if err != nil {
		t.Fatal(err)
	}
	user := models.User{ID: id, Status: models.StatusActive}
	writeScopes, err := auth.NewAccessScopes(auth.DefineAccessScope[models.User]("editorial.write"))
	if err != nil {
		t.Fatal(err)
	}
	approveScopes, err := auth.NewAccessScopes(auth.DefineAccessScope[models.User]("editorial.approve"))
	if err != nil {
		t.Fatal(err)
	}
	write := auth.DefinePermission("editorial.write", func(context.Context, models.User) (bool, error) { return false, nil })
	canApprove := true
	approve := auth.DefinePermission("editorial.approve", func(context.Context, models.User) (bool, error) { return canApprove, nil })
	provider := auth.DefineProvider("users", orm.IdentityOf(models.Users.Table, models.Users.Key()).ReferenceOf(user), func(context.Context, model.ID[models.User]) (value.Optional[models.User], error) {
		return value.Set(user), nil
	}, func(context.Context, models.User) (bool, error) { return true, nil })
	strategy := auth.DefineStrategy("bearer", func(_ context.Context, token secret.String) (value.Optional[auth.Proof[models.User, model.ID[models.User]]], error) {
		grants := approveScopes
		if token.Reveal() == "write" {
			grants = writeScopes
		}
		proof, err := authenticating.VerifiedScopedUser(user, grants)
		return value.Set(proof), err
	})
	guard := auth.DefineGuard("editorial", provider, strategy)
	registry, err := auth.NewRegistry(auth.DefaultConfig(), guard.Registration(), write.Registration(), approve.Registration(), authenticating.ReadOrder.Registration())
	if err != nil {
		t.Fatal(err)
	}
	transport, err := h.NewAuthentication(registry, h.BearerCredential("bearer"))
	if err != nil {
		t.Fatal(err)
	}
	endpoint := authenticating.EditorialProfile(authenticating.Profile(transport, guard), writeScopes, approveScopes, write, approve)
	router, err := h.NewRouter(authenticating.ProfileRoute(endpoint, guard))
	if err != nil {
		t.Fatal(err)
	}
	serve := func(token string) int {
		request := httptest.NewRequest("GET", "/profile", nil)
		request.Header.Set("Authorization", "Bearer "+token)
		response := httptest.NewRecorder()
		router.ServeHTTP(response, request)
		return response.Code
	}
	if status := serve("approve"); status != 204 {
		t.Fatal("approve branch denied", status)
	}
	if status := serve("write"); status != 403 {
		t.Fatal("crossed branch admitted", status)
	}
	canApprove = false
	if status := serve("approve"); status != 403 {
		t.Fatal("permission revocation ignored", status)
	}
	if err := h.ValidateAuthenticated(h.ConfigureAuthenticated(authenticating.Profile(transport, guard), authenticating.LegacyScopeOption(approveScopes))); err != nil {
		t.Fatal("legacy function-value wrapper", err)
	}
}
