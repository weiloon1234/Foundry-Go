package httppagination

import (
	"context"

	"foundry.test/consumer/internal/authfixture"
	"foundry.test/consumer/mutatorqueries"
	"github.com/weiloon1234/Foundry-Go/database/orm"
	foundryhttp "github.com/weiloon1234/Foundry-Go/http"
	"github.com/weiloon1234/Foundry-Go/http/pagination"
)

type Actor = authfixture.Actor

func guardedRoute(id foundryhttp.RouteID, path string) foundryhttp.Route[foundryhttp.NoPath] {
	return foundryhttp.DefineRoute(foundryhttp.RouteSpec{ID: id, Method: foundryhttp.GET, Access: foundryhttp.Guarded}, foundryhttp.StaticPath(path))
}

var GuardedList = pagination.DefineNumbered(guardedRoute("members.secure", "/members/secure"), MemberFiltersDescriptor(), MemberResponseJSON(), pagination.DefaultConfig())
var GuardedSimpleList = pagination.DefineSimple(guardedRoute("members.secure_simple", "/members/secure-simple"), MemberFiltersDescriptor(), MemberResponseJSON(), pagination.DefaultConfig())
var GuardedCursorList = pagination.DefineCursor[mutatorqueries.Member](guardedRoute("members.secure_cursor", "/members/secure-cursor"), MemberFiltersDescriptor(), MemberResponseJSON(), pagination.DefaultCursorConfig())

func AuthenticatedNumbered(binding foundryhttp.GuardBinding[Actor]) pagination.AuthenticatedNumberedEndpoint[foundryhttp.NoPath, MemberFilters, Actor, MemberResponse] {
	return pagination.Authenticated(GuardedList, binding)
}
func AuthenticatedSimple(binding foundryhttp.GuardBinding[Actor]) pagination.AuthenticatedSimpleEndpoint[foundryhttp.NoPath, MemberFilters, Actor, MemberResponse] {
	return pagination.Authenticated(GuardedSimpleList, binding)
}
func AuthenticatedCursor(binding foundryhttp.GuardBinding[Actor]) pagination.AuthenticatedCursorEndpoint[foundryhttp.NoPath, MemberFilters, mutatorqueries.Member, Actor, MemberResponse] {
	return pagination.Authenticated(GuardedCursorList, binding)
}

type AuthenticatedService interface {
	List(context.Context, Actor, ListRequest) (orm.PageResult[MemberResponse], error)
	SimpleList(context.Context, Actor, ListRequest) (orm.SimplePage[MemberResponse], error)
	CursorList(context.Context, Actor, CursorListRequest) (CursorListResult, error)
}

func AuthenticatedRouter(binding foundryhttp.GuardBinding[Actor], service AuthenticatedService) (*foundryhttp.Router, error) {
	endpoint := AuthenticatedNumbered(binding)
	return foundryhttp.NewRouter(pagination.HandleAuthenticated(endpoint, service.List), pagination.HandleAuthenticated(AuthenticatedSimple(binding), service.SimpleList), pagination.HandleAuthenticated(AuthenticatedCursor(binding), service.CursorList))
}
