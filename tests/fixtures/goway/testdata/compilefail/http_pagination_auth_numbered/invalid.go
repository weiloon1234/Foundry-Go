package invalid

import (
	"context"
	"foundry.test/goway/integrations"
	"github.com/weiloon1234/Foundry-Go/database/orm"
	foundryhttp "github.com/weiloon1234/Foundry-Go/http"
	"github.com/weiloon1234/Foundry-Go/http/pagination"
)

type OtherActor struct{ ID int64 }

var other foundryhttp.GuardBinding[OtherActor]

var _ = pagination.HandleAuthenticated(pagination.Authenticated(integrations.List, other, other.AnyActor()), func(context.Context, integrations.Actor, integrations.Request) (orm.PageResult[integrations.Response], error) {
	return orm.PageResult[integrations.Response]{}, nil
})
