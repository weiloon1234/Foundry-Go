package invalid

import (
	"context"
	"foundry.test/goway/integrations"
	foundryhttp "github.com/weiloon1234/Foundry-Go/http"
	"github.com/weiloon1234/Foundry-Go/http/pagination"
)

type OtherActor struct{ ID int64 }

var _ = pagination.Authenticated(integrations.List, foundryhttp.GuardBinding[integrations.Actor]{}).WithAuthorization(func(context.Context, OtherActor, integrations.Request) error { return nil })
