package invalid

import (
	"foundry.test/consumer/httppagination"
	"github.com/weiloon1234/Foundry-Go/auth"
	foundryhttp "github.com/weiloon1234/Foundry-Go/http"
)

type OtherActor struct{ ID int64 }

var _ = httppagination.AuthenticatedNumbered(foundryhttp.GuardBinding[httppagination.Actor]{}).WithScopes(auth.AccessScopes[OtherActor]{})
