package invalid

import (
	"foundry.test/goway/integrations"
	"github.com/weiloon1234/Foundry-Go/auth"
	"github.com/weiloon1234/Foundry-Go/database/orm"
	foundryhttp "github.com/weiloon1234/Foundry-Go/http"
	"github.com/weiloon1234/Foundry-Go/http/pagination"
)

type OtherActor struct{ ID int64 }

var _ = pagination.ConfigureAuthenticated[foundryhttp.NoPath, integrations.Filters, orm.PageRequest, integrations.Actor, orm.PageResult[integrations.Response], pagination.NumberedResponse[integrations.Response]](pagination.Authenticated(integrations.List, foundryhttp.GuardBinding[integrations.Actor]{}), foundryhttp.Permissions(auth.Permission[OtherActor]{}))
