package invalid

import (
	"context"
	"foundry.test/consumer/httppagination"
	foundryhttp "github.com/weiloon1234/Foundry-Go/http"
	"github.com/weiloon1234/Foundry-Go/http/pagination"
)

type OtherActor struct{ ID int64 }

var _ = pagination.Authenticated(httppagination.GuardedCursorList, foundryhttp.GuardBinding[OtherActor]{}).Handle(func(context.Context, httppagination.Actor, httppagination.CursorListRequest) (httppagination.CursorListResult, error) {
	return httppagination.CursorListResult{}, nil
})
