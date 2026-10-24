package invalid

import (
	"context"
	"foundry.test/goway/integrations"
	foundryhttp "github.com/weiloon1234/Foundry-Go/http"
	"github.com/weiloon1234/Foundry-Go/http/pagination"
)

type OtherActor struct{ ID int64 }

var _ = pagination.Authenticated(integrations.Cursor, foundryhttp.GuardBinding[OtherActor]{}).Handle(func(context.Context, integrations.Actor, integrations.CursorRequest) (integrations.CursorResult, error) {
	return integrations.CursorResult{}, nil
})
