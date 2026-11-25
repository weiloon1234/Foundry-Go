package invalid

import (
	"context"
	"foundry.test/consumer/httppagination"
	"github.com/weiloon1234/Foundry-Go/database/orm"
	foundryhttp "github.com/weiloon1234/Foundry-Go/http"
	"github.com/weiloon1234/Foundry-Go/http/pagination"
)

type OtherActor struct{ ID int64 }

var other foundryhttp.GuardBinding[OtherActor]

var _ = pagination.HandleAuthenticated(pagination.Authenticated(httppagination.GuardedSimpleList, other, other.AnyActor()), func(context.Context, httppagination.Actor, httppagination.ListRequest) (orm.SimplePage[httppagination.MemberResponse], error) {
	return orm.SimplePage[httppagination.MemberResponse]{}, nil
})
