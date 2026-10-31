// Package httppagination proves thin pagination with an independent consumer.
package httppagination

import (
	"context"

	"foundry.test/consumer/mutatorqueries"
	"github.com/weiloon1234/Foundry-Go/database"
	"github.com/weiloon1234/Foundry-Go/database/orm"
	foundryhttp "github.com/weiloon1234/Foundry-Go/http"
	"github.com/weiloon1234/Foundry-Go/http/pagination"
	"github.com/weiloon1234/Foundry-Go/model"
	"github.com/weiloon1234/Foundry-Go/value"
)

//foundry:query
type MemberFilters struct {
	Email value.Optional[string] `query:"email"`
}

//foundry:dto
type MemberResponse struct {
	ID       model.ID[mutatorqueries.Member]                `json:"id"`
	Email    mutatorqueries.DisplayEmail                    `json:"email"`
	Nickname value.Nullable[mutatorqueries.DisplayNickname] `json:"nickname"`
}

type ListRequest = pagination.Request[foundryhttp.NoPath, MemberFilters]

var List = pagination.DefineNumbered(
	foundryhttp.DefineRoute(foundryhttp.RouteSpec{ID: "members.index", Method: foundryhttp.GET, Access: foundryhttp.Public}, foundryhttp.StaticPath("/members")),
	MemberFiltersDescriptor(), MemberResponseJSON(), pagination.DefaultConfig(),
)

type Service interface {
	List(context.Context, ListRequest) (orm.PageResult[MemberResponse], error)
}

func Router(service Service) (*foundryhttp.Router, error) {
	return foundryhttp.NewRouter(pagination.Handle(List, service.List))
}

// DatabaseService uses the handwritten ORM declarations with the framework's parsed
// page request. The endpoint owns page metadata, JSON contracts and links.
type DatabaseService struct{ DB database.Executor }

func (s DatabaseService) List(ctx context.Context, in ListRequest) (orm.PageResult[MemberResponse], error) {
	builder := memberQuery(in.Filters)
	page, err := orm.Paginate(ctx, s.DB, builder, in.Page)
	if err != nil {
		return orm.PageResult[MemberResponse]{}, err
	}
	return pagination.MapPage(ctx, page, PresentMember)
}

// PresentMember explicitly chooses read accessors. Persisted identity stays
// model-owned and untouched; a getter failure aborts the complete response.
func PresentMember(member mutatorqueries.Member) (MemberResponse, error) {
	email, err := member.AccessEmail()
	if err != nil {
		return MemberResponse{}, err
	}
	nickname, err := member.AccessNickname()
	if err != nil {
		return MemberResponse{}, err
	}
	return MemberResponse{ID: member.ID, Email: email, Nickname: nickname}, nil
}

func memberQuery(filters MemberFilters) orm.Query[mutatorqueries.Member] {
	builder := mutatorqueries.Members.Query()
	if email, set := filters.Email.Get(); set {
		builder = builder.Where(orm.InputTextEq(mutatorqueries.Members.Email, email))
	}
	return builder
}
