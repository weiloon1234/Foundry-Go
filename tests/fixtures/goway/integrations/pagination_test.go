package integrations_test

import (
	"context"
	"encoding/base64"
	"encoding/json"
	"net/http/httptest"
	"reflect"
	"strings"
	"testing"

	"foundry.test/goway/integrations"
	"foundry.test/goway/models"
	"github.com/weiloon1234/Foundry-Go/database/orm"
	"github.com/weiloon1234/Foundry-Go/database/query"
	foundryhttp "github.com/weiloon1234/Foundry-Go/http"
	"github.com/weiloon1234/Foundry-Go/http/pagination"
	"github.com/weiloon1234/Foundry-Go/value"
)

func TestORMPaginationKeepsTransportIdentity(t *testing.T) {
	for _, pair := range [][2]reflect.Type{
		{reflect.TypeFor[orm.PageResult[models.User]](), reflect.TypeFor[query.Page[models.User]]()},
		{reflect.TypeFor[orm.SimplePage[models.User]](), reflect.TypeFor[query.SimplePage[models.User]]()},
		{reflect.TypeFor[orm.Cursor[models.User]](), reflect.TypeFor[query.Cursor[models.User]]()},
		{reflect.TypeFor[orm.CursorRequest[models.User]](), reflect.TypeFor[query.CursorRequest[models.User]]()},
		{reflect.TypeFor[orm.CursorPage[models.User]](), reflect.TypeFor[query.CursorPage[models.User]]()},
		{reflect.TypeFor[orm.CursorInputError](), reflect.TypeFor[query.CursorInputError]()},
	} {
		if pair[0] != pair[1] {
			t.Fatalf("transport identity changed: %v", pair)
		}
	}
	token := base64.RawURLEncoding.EncodeToString([]byte(`{"v":1,"scope":"` + strings.Repeat("a", 64) + `","values":[{"k":"int","t":"42"}]}`))
	if _, err := orm.ParseCursor[models.User](token); err != nil {
		t.Fatal(err)
	}
	// Valid tokens and invalid structures retain the legacy parse result.
	for _, input := range []string{token, "", "bad!"} {
		old, oldErr := query.ParseCursor[models.User](input)
		current, err := orm.ParseCursor[models.User](input)
		if (oldErr == nil) != (err == nil) || old.Token() != current.Token() {
			t.Fatal("cursor transport changed")
		}
	}
}

func TestORMPagesReachHTTPWithoutExposingModels(t *testing.T) {
	registration := integrations.List.Handle(func(ctx context.Context, request integrations.Request) (orm.PageResult[integrations.Response], error) {
		return pagination.MapPage(ctx, orm.PageResult[models.User]{Items: []models.User{{Email: "visible@example.test"}}, Number: request.Page.Number, Size: request.Page.Size, Total: 1, Pages: 1}, integrations.Present)
	})
	router, err := foundryhttp.NewRouter(registration)
	if err != nil {
		t.Fatal(err)
	}
	response := httptest.NewRecorder()
	router.ServeHTTP(response, httptest.NewRequest("GET", "/orm-users", nil))
	var body pagination.NumberedResponse[integrations.Response]
	if response.Code != 200 || json.Unmarshal(response.Body.Bytes(), &body) != nil || !reflect.DeepEqual(body.Data, []integrations.Response{{Email: "visible@example.test"}}) {
		t.Fatalf("ORM HTTP page: %d %s", response.Code, response.Body.String())
	}
	if _, err := pagination.MapCursorPage(t.Context(), orm.CursorPage[models.User]{Items: []models.User{{Email: "cursor@example.test"}}, Size: 20}, integrations.Present); err != nil {
		t.Fatal(err)
	}
	var cursor orm.Cursor[models.User]
	if _, err := integrations.Cursor.URL(t.Context(), foundryhttp.NoPath{}, integrations.Filters{}, orm.CursorRequest[models.User]{Size: 20, After: value.Set(cursor)}); err == nil {
		t.Fatal("zero cursor accepted")
	}
}
