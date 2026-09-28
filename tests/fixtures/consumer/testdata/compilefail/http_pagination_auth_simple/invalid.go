package invalid

import (
	"context"
	"foundry.test/consumer/httppagination"
	"github.com/weiloon1234/Foundry-Go/database/query"
	foundryhttp "github.com/weiloon1234/Foundry-Go/http"
	"github.com/weiloon1234/Foundry-Go/http/pagination"
)

type OtherActor struct{ ID int64 }

var _ = pagination.Authenticated(httppagination.GuardedSimpleList, foundryhttp.GuardBinding[OtherActor]{}).Handle(func(context.Context, httppagination.Actor, httppagination.ListRequest) (query.SimplePage[httppagination.MemberResponse], error) {
	return query.SimplePage[httppagination.MemberResponse]{}, nil
})
