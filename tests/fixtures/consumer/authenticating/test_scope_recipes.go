package authenticating

import (
	"testing"

	"foundry.test/consumer/models"
	"github.com/weiloon1234/Foundry-Go/auth"
	"github.com/weiloon1234/Foundry-Go/auth/session"
	"github.com/weiloon1234/Foundry-Go/model"
	authtest "github.com/weiloon1234/Foundry-Go/testkit/auth"
)

func requireOrderInTest(t testing.TB, registry *auth.Registry, api auth.Guard[models.User], issued session.Issued[models.User, model.ID[models.User]], order models.Order) models.User {
	// doc-example:begin test-credential-scope
	scope := authtest.Scope(t, registry,
		auth.Credential{Name: api.Source(), Secret: issued.Secret()},
	)
	user := authtest.Require(t, scope, api)
	// Check resource policy independently; authentication is not authorization.
	if err := ReadOrder.Authorize(scope.Context(), api, order); err != nil {
		t.Fatal(err)
	}
	// doc-example:end test-credential-scope
	return user
}
