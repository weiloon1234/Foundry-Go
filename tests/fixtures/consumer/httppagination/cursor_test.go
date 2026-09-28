package httppagination_test

import (
	"context"
	"encoding/base64"
	"net/http/httptest"
	"net/url"
	"strings"
	"testing"

	"foundry.test/consumer/httppagination"
	"foundry.test/consumer/mutatorqueries"
	"github.com/weiloon1234/Foundry-Go/database/query"
	foundryhttp "github.com/weiloon1234/Foundry-Go/http"
	"github.com/weiloon1234/Foundry-Go/http/pagination"
	"github.com/weiloon1234/Foundry-Go/model"
	"github.com/weiloon1234/Foundry-Go/value"
)

type cursorService struct {
	member mutatorqueries.Member
	token  query.Cursor[mutatorqueries.Member]
	t      *testing.T
}

func (s cursorService) CursorList(ctx context.Context, in httppagination.CursorListRequest) (httppagination.CursorListResult, error) {
	s.t.Helper()
	if email, set := in.Filters.Email.Get(); !set || email != "member+filter@example.test" {
		s.t.Error("generated cursor filter lost")
	}
	if in.Page.Size != 20 || !in.Page.After.IsSet() || in.Page.Before.IsSet() {
		s.t.Error("typed cursor request lost")
	}
	return pagination.MapCursorPage(ctx, query.CursorPage[mutatorqueries.Member]{Items: []mutatorqueries.Member{s.member}, Size: in.Page.Size, Next: value.Set(s.token)}, httppagination.PresentMember)
}
func TestGeneratedCursorEndpointPreservesModelAndDTOOwners(t *testing.T) {
	t.Parallel()
	id, err := model.ParseID[mutatorqueries.Member]("0193fd8c-2075-7000-8000-000000000001")
	if err != nil {
		t.Fatal(err)
	}
	wire := `{"v":1,"scope":"` + strings.Repeat("a", 64) + `","values":[{"k":"string","t":"` + id.String() + `"}]}`
	token, err := query.ParseCursor[mutatorqueries.Member](base64.RawURLEncoding.EncodeToString([]byte(wire)))
	if err != nil {
		t.Fatal(err)
	}
	member := mutatorqueries.Member{ID: id, Email: "stored@example.test"}
	router, err := httppagination.CursorRouter(cursorService{member: member, token: token, t: t})
	if err != nil {
		t.Fatal(err)
	}
	location, err := httppagination.CursorList.URL(t.Context(), foundryhttp.NoPath{}, httppagination.MemberFilters{Email: value.Set("member+filter@example.test")}, query.CursorRequest[mutatorqueries.Member]{Size: 20, After: value.Set(token)})
	if err != nil {
		t.Fatal(err)
	}
	response := httptest.NewRecorder()
	router.ServeHTTP(response, httptest.NewRequest("GET", location, nil))
	if response.Code != 200 {
		t.Fatalf("cursor response: %d %s", response.Code, response.Body)
	}
	got, err := pagination.CursorJSON(httppagination.MemberResponseJSON()).Decode(t.Context(), response.Body.Bytes(), foundryhttp.DefaultEndpointLimits().Response)
	if err != nil {
		t.Fatal(err)
	}
	if len(got.Data) != 1 || got.Data[0].ID != id || got.Data[0].Email != "STORED@EXAMPLE.TEST" || member.Email != "stored@example.test" {
		t.Fatal("cursor presentation changed stored fields or DTO getter")
	}
	next, _ := got.Links.Next.Get()
	parsed, err := url.Parse(next)
	if err != nil {
		t.Fatal(err)
	}
	if parsed.Query().Get("after") != token.Token() || parsed.Query().Get("email") != "member+filter@example.test" {
		t.Fatal("source cursor/filter changed")
	}
}
