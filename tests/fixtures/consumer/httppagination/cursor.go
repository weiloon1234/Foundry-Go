package httppagination

import (
	"context"

	"github.com/weiloon1234/Foundry-Go/database/orm"
	foundryhttp "github.com/weiloon1234/Foundry-Go/http"
	"github.com/weiloon1234/Foundry-Go/http/pagination"
)

// doc-example:begin http-cursor-pagination-cursor-list-request
type CursorListRequest = pagination.CursorRequest[foundryhttp.NoPath, MemberFilters, Member]
type CursorListResult = pagination.CursorResult[Member, MemberResponse]

var CursorList = func() pagination.CursorEndpoint[foundryhttp.NoPath, MemberFilters, Member, MemberResponse] {
	membersCursorRoute := foundryhttp.DefineRoute(foundryhttp.RouteSpec{ID: "members.cursor", Method: foundryhttp.GET, Access: foundryhttp.Public}, foundryhttp.StaticPath("/members/cursor"))
	return pagination.DefineCursor[Member](
		membersCursorRoute,
		MemberFiltersDescriptor(), MemberResponseJSON(), pagination.DefaultCursorConfig(),
	)
}()

// doc-example:end http-cursor-pagination-cursor-list-request

type CursorService interface {
	CursorList(context.Context, CursorListRequest) (CursorListResult, error)
}

func CursorRouter(service CursorService) (*foundryhttp.Router, error) {
	return foundryhttp.NewRouter(pagination.Handle(CursorList, service.CursorList))
}

// doc-example:begin http-cursor-pagination-example-2
func (s DatabaseService) CursorList(ctx context.Context, in CursorListRequest) (CursorListResult, error) {
	page, err := orm.CursorPaginate(ctx, s.DB, memberQuery(in.Filters), in.Page)
	if err != nil {
		return CursorListResult{}, err
	}
	return pagination.MapCursorPage(ctx, page, PresentMember)
}

// doc-example:end http-cursor-pagination-example-2
