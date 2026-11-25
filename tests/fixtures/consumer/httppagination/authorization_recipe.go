package httppagination

import (
	"context"

	"github.com/weiloon1234/Foundry-Go/auth"
	"github.com/weiloon1234/Foundry-Go/database/orm"
	foundryhttp "github.com/weiloon1234/Foundry-Go/http"
	"github.com/weiloon1234/Foundry-Go/http/pagination"
)

// doc-example:begin authorized-list-service
type authorizedListService interface {
	AuthorizeList(context.Context, Actor, pagination.Request[foundryhttp.NoPath, MemberFilters]) error
	List(context.Context, Actor, pagination.Request[foundryhttp.NoPath, MemberFilters]) (orm.PageResult[MemberResponse], error)
}

// doc-example:end authorized-list-service

func registerMemberList(list pagination.NumberedEndpoint[foundryhttp.NoPath, MemberFilters, MemberResponse], binding foundryhttp.GuardBinding[Actor], readScopes auth.AccessScopes[Actor], readPermission auth.Permission[Actor], service authorizedListService) foundryhttp.RouteRegistration {
	// doc-example:begin authorized-secured-list
	secured := pagination.Authenticated(list, binding, foundryhttp.AllOf(foundryhttp.Scopes(readScopes), foundryhttp.Permissions(readPermission)))
	secured = pagination.AuthorizeActor(secured, service.AuthorizeList)
	return pagination.HandleAuthenticated(secured, service.List)
	// doc-example:end authorized-secured-list
}
