package authenticating_test

import (
	"context"
	"errors"
	"sync/atomic"
	"testing"

	"foundry.test/consumer/authenticating"
	"foundry.test/consumer/models"
	"github.com/weiloon1234/Foundry-Go/auth"
	"github.com/weiloon1234/Foundry-Go/model"
	"github.com/weiloon1234/Foundry-Go/secret"
	"github.com/weiloon1234/Foundry-Go/value"
)

func TestConsumerAccessScopesKeepGeneratedModelAndResourcePolicy(t *testing.T) {
	id, err := model.NewID[models.User]()
	if err != nil {
		t.Fatal(err)
	}
	user := models.User{ID: id, Status: models.StatusActive}
	grants, err := authenticating.OrderReadScopes()
	if err != nil {
		t.Fatal(err)
	}
	proof, err := authenticating.VerifiedScopedUser(user, grants)
	if err != nil {
		t.Fatal(err)
	}
	var loads atomic.Int32
	provider := auth.DefineProvider("users", user.FoundryReference(), func(_ context.Context, key model.ID[models.User]) (value.Optional[models.User], error) {
		loads.Add(1)
		return value.Set(user), nil
	}, func(context.Context, models.User) (bool, error) { return true, nil })
	strategy := auth.DefineStrategy("bearer", func(context.Context, secret.String) (value.Optional[auth.Proof[models.User, model.ID[models.User]]], error) {
		return value.Set(proof), nil
	})
	registry, api, _, err := authenticating.AccountGuards(provider, strategy, strategy)
	if err != nil {
		t.Fatal(err)
	}
	credentials, err := auth.NewCredentials(auth.Credential{Name: "bearer", Secret: secret.New("verified-fixture")})
	if err != nil {
		t.Fatal(err)
	}
	scope, err := registry.NewScope(t.Context(), credentials)
	if err != nil {
		t.Fatal(err)
	}
	defer scope.Close()
	subject, err := authenticating.ReadScopedOrder(scope.Context(), api, grants, models.Order{BuyerID: id})
	if err != nil || subject.ID != id {
		t.Fatal(subject, err)
	}
	other, err := model.NewID[models.User]()
	if err != nil {
		t.Fatal(err)
	}
	subject, err = authenticating.ReadScopedOrder(scope.Context(), api, grants, models.Order{BuyerID: other})
	if !errors.Is(err, auth.Forbidden) || !subject.ID.IsZero() {
		t.Fatal("scopes bypassed resource policy", err)
	}
	names := authenticating.GrantedScopeNames(grants)
	names[0] = "changed"
	if !grants.Contains(authenticating.OrderReadScope) || loads.Load() != 1 {
		t.Fatal("grant mutated or provider repeated")
	}
}
