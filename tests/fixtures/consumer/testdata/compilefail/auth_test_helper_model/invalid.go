package invalid

import (
	"foundry.test/consumer/models"
	"github.com/weiloon1234/Foundry-Go/auth"
	authtest "github.com/weiloon1234/Foundry-Go/testkit/auth"
	"testing"
)

func invalid(t *testing.T, scope *auth.Scope, guard auth.Guard[models.Order]) {
	_ = authtest.Require[models.User](t, scope, guard)
}
