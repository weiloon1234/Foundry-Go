package invalid

import (
	"foundry.test/consumer/httppagination"
	"github.com/weiloon1234/Foundry-Go/auth"
	"github.com/weiloon1234/Foundry-Go/database/orm"
	foundryhttp "github.com/weiloon1234/Foundry-Go/http"
	pagination "github.com/weiloon1234/Foundry-Go/http/pagination"
)

type OtherActor struct{ ID int64 }

var _ = pagination.ConfigureAuthenticated[foundryhttp.NoPath, httppagination.MemberFilters, orm.PageRequest, httppagination.Actor, orm.PageResult[httppagination.MemberResponse], pagination.NumberedResponse[httppagination.MemberResponse]](httppagination.AuthenticatedNumbered(foundryhttp.GuardBinding[httppagination.Actor]{}), foundryhttp.Permissions(auth.Permission[OtherActor]{}))
