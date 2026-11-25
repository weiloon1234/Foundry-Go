package invalid

import (
	"foundry.test/consumer/httppagination"
	"github.com/weiloon1234/Foundry-Go/auth"
	foundryhttp "github.com/weiloon1234/Foundry-Go/http"
	pagination "github.com/weiloon1234/Foundry-Go/http/pagination"
)

type OtherActor struct{ ID int64 }

var binding foundryhttp.GuardBinding[httppagination.Actor]

var _ = pagination.Authenticated(httppagination.GuardedList, binding, foundryhttp.AllOf(foundryhttp.Scopes(auth.AccessScopes[OtherActor]{})))
