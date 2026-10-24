package invalid

import (
	"context"
	"foundry.test/goway/integrations"
	"github.com/weiloon1234/Foundry-Go/database/orm"
	foundryhttp "github.com/weiloon1234/Foundry-Go/http"
	"github.com/weiloon1234/Foundry-Go/http/pagination"
)

type OtherActor struct{ ID int64 }

var _ = pagination.Authenticated(integrations.Simple, foundryhttp.GuardBinding[OtherActor]{}).Handle(func(context.Context, integrations.Actor, integrations.Request) (orm.SimplePage[integrations.Response], error) {
	return orm.SimplePage[integrations.Response]{}, nil
})
