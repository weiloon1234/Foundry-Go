package httppagination

import (
	"context"

	"github.com/weiloon1234/Foundry-Go/auth"
	"github.com/weiloon1234/Foundry-Go/database/orm"
	http "github.com/weiloon1234/Foundry-Go/http"
	"github.com/weiloon1234/Foundry-Go/http/pagination"
)

// doc-example:begin authorized-list-service
type authorizedListService interface {
	AuthorizeList(context.Context, Actor, pagination.Request[http.NoPath, MemberFilters]) error
	List(context.Context, Actor, pagination.Request[http.NoPath, MemberFilters]) (orm.PageResult[MemberResponse], error)
}

// doc-example:end authorized-list-service

func registerMemberList(list pagination.NumberedEndpoint[http.NoPath, MemberFilters, MemberResponse], binding http.GuardBinding[Actor], readScopes auth.AccessScopes[Actor], readPermission auth.Permission[Actor], service authorizedListService) http.RouteRegistration {
	// doc-example:begin authenticated-pagination-20260928-1
	secured := pagination.Authenticated(list, binding, http.Scopes(readScopes), http.Permissions(readPermission))
	secured = pagination.AuthorizeActor(secured, service.AuthorizeList)
	registration := pagination.HandleAuthenticated(secured, service.List)
	// doc-example:end authenticated-pagination-20260928-1
	return registration
}
