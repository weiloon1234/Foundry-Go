package httppagination_test

import (
	"context"
	"encoding/json"
	"net/http/httptest"
	"net/url"
	"testing"

	"foundry.test/consumer/httppagination"
	"foundry.test/consumer/mutatorqueries"
	"github.com/weiloon1234/Foundry-Go/database/query"
	"github.com/weiloon1234/Foundry-Go/http/pagination"
	"github.com/weiloon1234/Foundry-Go/model"
	"github.com/weiloon1234/Foundry-Go/value"
)

type service struct {
	member mutatorqueries.Member
	t      *testing.T
}

func (s service) List(ctx context.Context, in httppagination.ListRequest) (query.Page[httppagination.MemberResponse], error) {
	s.t.Helper()
	if email, set := in.Filters.Email.Get(); !set || email != "member+filter@example.test" {
		s.t.Error("generated filters changed")
	}
	return pagination.MapPage(ctx, query.Page[mutatorqueries.Member]{Items: []mutatorqueries.Member{s.member}, Number: in.Page.Number, Size: in.Page.Size, Total: 21, Pages: 2}, httppagination.PresentMember)
}
func TestGeneratedPageEndpointPreservesGettersAndContracts(t *testing.T) {
	t.Parallel()
	id, err := model.ParseID[mutatorqueries.Member]("0193fd8c-2075-7000-8000-000000000001")
	if err != nil {
		t.Fatal(err)
	}
	member := mutatorqueries.Member{ID: id, Email: "stored@example.test", Nickname: value.Of("nickname")}
	router, err := httppagination.Router(service{member: member, t: t})
	if err != nil {
		t.Fatal(err)
	}
	response := httptest.NewRecorder()
	router.ServeHTTP(response, httptest.NewRequest("GET", "/members?email=member%2Bfilter%40example.test", nil))
	if response.Code != 200 {
		t.Fatalf("status=%d body=%s", response.Code, response.Body)
	}
	var got pagination.NumberedResponse[httppagination.MemberResponse]
	if err := json.Unmarshal(response.Body.Bytes(), &got); err != nil {
		t.Fatal(err)
	}
	if len(got.Data) != 1 || got.Data[0].ID != id || got.Data[0].Email != "STORED@EXAMPLE.TEST" {
		t.Fatalf("presentation=%+v", got)
	}
	if nick, _ := got.Data[0].Nickname.Get(); nick != "NICKNAME" {
		t.Fatal("nullable getter result lost")
	}
	if member.Email != "stored@example.test" || member.ID != id {
		t.Fatal("stored fields changed")
	}
	next, ok := got.Links.Next.Get()
	if !ok {
		t.Fatal("missing next page")
	}
	target, err := url.Parse(next)
	if err != nil {
		t.Fatal(err)
	}
	if target.Query().Get("email") != "member+filter@example.test" || target.Query().Get("page") != "2" {
		t.Fatal("generated filter link lost")
	}
	info, err := httppagination.List.Description()
	if err != nil || info.Response == nil {
		t.Fatal("page contract missing", err)
	}
}
