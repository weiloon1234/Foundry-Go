// Package integrations exercises framework adapters with handwritten ORM models.
package integrations

import (
	"context"

	"foundry.test/goway/models"
	"github.com/weiloon1234/Foundry-Go/database"
	"github.com/weiloon1234/Foundry-Go/database/orm"
	foundryhttp "github.com/weiloon1234/Foundry-Go/http"
	"github.com/weiloon1234/Foundry-Go/http/pagination"
)

//foundry:dto
type Response struct {
	Email string `json:"email"`
}

type Actor struct{ ID int64 }
type Filters struct{ Search string }
type Request = pagination.Request[foundryhttp.NoPath, Filters]
type CursorRequest = pagination.CursorRequest[foundryhttp.NoPath, Filters, models.User]
type CursorResult = pagination.CursorResult[models.User, Response]

var query = foundryhttp.DefineQuery(foundryhttp.DefaultQueryParam("search", foundryhttp.StringQuery[string](), "", func(f *Filters) *string { return &f.Search }))
var item = ResponseJSON()
var route = foundryhttp.DefineRoute(foundryhttp.RouteSpec{ID: "orm.users", Method: foundryhttp.GET, Access: foundryhttp.Public}, foundryhttp.StaticPath("/orm-users"))
var List = pagination.DefineNumbered(route, query, item, pagination.DefaultConfig())
var Simple = pagination.DefineSimple(route, query, item, pagination.DefaultConfig())
var Cursor = pagination.DefineCursor[models.User](route, query, item, pagination.DefaultCursorConfig())

func Present(user models.User) (Response, error) { return Response{Email: user.Email}, nil }
func Read(ctx context.Context, db database.Executor, request Request) (orm.PageResult[Response], error) {
	page, err := orm.Paginate(ctx, db, models.Users.Query(), request.Page)
	if err != nil {
		return orm.PageResult[Response]{}, err
	}
	return pagination.MapPage(ctx, page, Present)
}
func ReadCursor(ctx context.Context, db database.Executor, request CursorRequest) (CursorResult, error) {
	page, err := orm.CursorPaginate(ctx, db, models.Users.Query(), request.Page)
	if err != nil {
		return CursorResult{}, err
	}
	return pagination.MapCursorPage(ctx, page, Present)
}
