package invalid

import (
	"context"
	"foundry.test/consumer/httppagination"
	"github.com/weiloon1234/Foundry-Go/database/orm"
	foundryhttp "github.com/weiloon1234/Foundry-Go/http"
	"github.com/weiloon1234/Foundry-Go/http/pagination"
)

type OtherActor struct{ ID int64 }

var _ = pagination.HandleAuthenticated(pagination.Authenticated(httppagination.GuardedList, foundryhttp.GuardBinding[OtherActor]{}), func(context.Context, httppagination.Actor, httppagination.ListRequest) (orm.PageResult[httppagination.MemberResponse], error) {
	return orm.PageResult[httppagination.MemberResponse]{}, nil
})
