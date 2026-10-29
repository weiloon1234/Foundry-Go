package invalid

import (
	"context"

	"foundry.test/consumer/httppagination"
	"github.com/weiloon1234/Foundry-Go/database/orm"
	foundryhttp "github.com/weiloon1234/Foundry-Go/http"
	pagination "github.com/weiloon1234/Foundry-Go/http/pagination"
)

type OtherActor struct{ ID int64 }

var _ = pagination.AuthorizeActor[foundryhttp.NoPath, httppagination.MemberFilters, orm.PageRequest, httppagination.Actor, orm.PageResult[httppagination.MemberResponse], pagination.NumberedResponse[httppagination.MemberResponse]](httppagination.AuthenticatedNumbered(foundryhttp.GuardBinding[httppagination.Actor]{}), func(context.Context, OtherActor, httppagination.ListRequest) error { return nil })
