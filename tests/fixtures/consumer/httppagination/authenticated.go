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

var GuardedList = func() pagination.NumberedEndpoint[foundryhttp.NoPath, MemberFilters, MemberResponse] {
	membersSecureRoute := guardedRoute("members.secure", "/members/secure")
	return pagination.DefineNumbered(membersSecureRoute, MemberFiltersDescriptor(), MemberResponseJSON(), pagination.DefaultConfig())
}()
var GuardedSimpleList = func() pagination.SimpleEndpoint[foundryhttp.NoPath, MemberFilters, MemberResponse] {
	membersSecureSimpleRoute := guardedRoute("members.secure_simple", "/members/secure-simple")
	return pagination.DefineSimple(membersSecureSimpleRoute, MemberFiltersDescriptor(), MemberResponseJSON(), pagination.DefaultConfig())
}()
var GuardedCursorList = func() pagination.CursorEndpoint[foundryhttp.NoPath, MemberFilters, mutatorqueries.Member, MemberResponse] {
	membersSecureCursorRoute := guardedRoute("members.secure_cursor", "/members/secure-cursor")
	return pagination.DefineCursor[mutatorqueries.Member](membersSecureCursorRoute, MemberFiltersDescriptor(), MemberResponseJSON(), pagination.DefaultCursorConfig())
}()

// doc-example:begin http-pagination-authenticated-numbered
func AuthenticatedNumbered(guarded pagination.NumberedEndpoint[foundryhttp.NoPath, MemberFilters, MemberResponse], binding foundryhttp.GuardBinding[Actor]) pagination.AuthenticatedNumberedEndpoint[foundryhttp.NoPath, MemberFilters, Actor, MemberResponse] {
	return pagination.Authenticated(guarded, binding, binding.AnyActor())
}

// doc-example:end http-pagination-authenticated-numbered
func AuthenticatedSimple(binding foundryhttp.GuardBinding[Actor]) pagination.AuthenticatedSimpleEndpoint[foundryhttp.NoPath, MemberFilters, Actor, MemberResponse] {
	return pagination.Authenticated(GuardedSimpleList, binding, binding.AnyActor())
}
func AuthenticatedCursor(binding foundryhttp.GuardBinding[Actor]) pagination.AuthenticatedCursorEndpoint[foundryhttp.NoPath, MemberFilters, mutatorqueries.Member, Actor, MemberResponse] {
	return pagination.Authenticated(GuardedCursorList, binding, binding.AnyActor())
}

type AuthenticatedService interface {
	List(context.Context, Actor, ListRequest) (orm.PageResult[MemberResponse], error)
	SimpleList(context.Context, Actor, ListRequest) (orm.SimplePage[MemberResponse], error)
	CursorList(context.Context, Actor, CursorListRequest) (CursorListResult, error)
}

func AuthenticatedRouter(binding foundryhttp.GuardBinding[Actor], service AuthenticatedService) (*foundryhttp.Router, error) {
	endpoint := AuthenticatedNumbered(GuardedList, binding)

	membersSecureSimpleEndpoint := AuthenticatedSimple(binding)
	membersSecureCursorEndpoint := AuthenticatedCursor(binding)

	return foundryhttp.NewRouter(pagination.HandleAuthenticated(endpoint, service.List), pagination.HandleAuthenticated(membersSecureSimpleEndpoint, service.SimpleList), pagination.HandleAuthenticated(membersSecureCursorEndpoint, service.CursorList))
}
