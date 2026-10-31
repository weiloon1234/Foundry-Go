package httppagination

import (
	"context"

	"foundry.test/consumer/mutatorqueries"
	"github.com/weiloon1234/Foundry-Go/database/orm"
	foundryhttp "github.com/weiloon1234/Foundry-Go/http"
	"github.com/weiloon1234/Foundry-Go/http/pagination"
)

type CursorListRequest = pagination.CursorRequest[foundryhttp.NoPath, MemberFilters, mutatorqueries.Member]
type CursorListResult = pagination.CursorResult[mutatorqueries.Member, MemberResponse]

var CursorList = pagination.DefineCursor[mutatorqueries.Member](
	foundryhttp.DefineRoute(foundryhttp.RouteSpec{ID: "members.cursor", Method: foundryhttp.GET, Access: foundryhttp.Public}, foundryhttp.StaticPath("/members/cursor")),
	MemberFiltersDescriptor(), MemberResponseJSON(), pagination.DefaultCursorConfig(),
)

type CursorService interface {
	CursorList(context.Context, CursorListRequest) (CursorListResult, error)
}

func CursorRouter(service CursorService) (*foundryhttp.Router, error) {
	return foundryhttp.NewRouter(pagination.Handle(CursorList, service.CursorList))
}

func (s DatabaseService) CursorList(ctx context.Context, in CursorListRequest) (CursorListResult, error) {
	page, err := orm.CursorPaginate(ctx, s.DB, memberQuery(in.Filters), in.Page)
	if err != nil {
		return CursorListResult{}, err
	}
	return pagination.MapCursorPage(ctx, page, PresentMember)
}
