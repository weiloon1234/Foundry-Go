package invalid

import (
	"context"
	"foundry.test/consumer/httppagination"
	foundryhttp "github.com/weiloon1234/Foundry-Go/http"
	"github.com/weiloon1234/Foundry-Go/http/pagination"
)

type OtherActor struct{ ID int64 }

var other foundryhttp.GuardBinding[OtherActor]

var _ = pagination.HandleAuthenticated(pagination.Authenticated(httppagination.GuardedCursorList, other, other.AnyActor()), func(context.Context, httppagination.Actor, httppagination.CursorListRequest) (httppagination.CursorListResult, error) {
	return httppagination.CursorListResult{}, nil
})
