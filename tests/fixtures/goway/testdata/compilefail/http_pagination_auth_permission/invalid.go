package invalid

import (
	"foundry.test/goway/integrations"
	"github.com/weiloon1234/Foundry-Go/auth"
	foundryhttp "github.com/weiloon1234/Foundry-Go/http"
	"github.com/weiloon1234/Foundry-Go/http/pagination"
)

type OtherActor struct{ ID int64 }

var binding foundryhttp.GuardBinding[integrations.Actor]

var _ = pagination.Authenticated(integrations.List, binding, foundryhttp.AllOf(foundryhttp.Permissions(auth.Permission[OtherActor]{})))
